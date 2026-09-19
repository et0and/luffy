// Command luffy is a single binary replacement for the PirateBox Python
// release. It serves the web interface, chat, board, uploads, directory
// listings and runs the same network setup the shell scripts did.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"luffy/internal/config"
	"luffy/internal/diskusage"
	"luffy/internal/logging"
	"luffy/internal/proc"
	"luffy/internal/service"
	"luffy/internal/shoutbox"
	"luffy/internal/stations"
	"luffy/internal/timesave"
	"luffy/internal/web"
)

// version is injected at build time with -ldflags "-X main.version=...".
var version = "dev"

const usageText = `luffy - PirateBox-compatible file share and chat server

Usage:
  luffy [global flags] <command>

Commands:
  serve        start the web server (and network setup)
  start        full start: network, services, web server (default)
  stop         stop network services and background helpers
  status       show the current configuration and service state
  setup        install: create folders, links and generated configs
  shoutbox     chat helper commands
  timesave     store or recover the clock on devices without an RTC
  stations     print the number of connected clients
  hostname     change the visible hostname
  flush-dns    signal dnsmasq to reload its hosts files
  version      print the version

Global flags:
  --config PATH   path to piratebox.conf (default /opt/piratebox/conf/piratebox.conf)
  --log-level L   debug, info, warn or error (default info)
  --dry-run       print what would be executed without running it
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	global := flag.NewFlagSet("luffy", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	configPath := global.String("config", config.DefaultPath, "path to piratebox.conf")
	logLevel := global.String("log-level", "info", "debug, info, warn or error")
	dryRun := global.Bool("dry-run", false, "print commands without executing them")
	showVersion := global.Bool("version", false, "print the version and exit")
	if err := global.Parse(args); err != nil {
		fmt.Fprint(os.Stderr, usageText)
		return 2
	}
	if *showVersion {
		fmt.Println("luffy " + version)
		return 0
	}

	logger := logging.Setup(os.Stderr, *logLevel)

	rest := global.Args()
	command := "start"
	if len(rest) > 0 {
		command = rest[0]
		rest = rest[1:]
	}
	if command == "version" {
		fmt.Println("luffy " + version)
		return 0
	}
	if command == "help" || command == "-h" || command == "--help" {
		fmt.Print(usageText)
		return 0
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("cannot load configuration", "error", err)
		return 1
	}
	for _, warning := range cfg.Warnings() {
		logger.Warn("config warning", "detail", warning)
	}

	runner := proc.Runner(proc.Exec{LogDir: filepath.Join(cfg.PirateboxFolder, "tmp")})
	if *dryRun {
		runner = &dryRunRunner{logger: logger}
	}
	manager := service.New(cfg, runner, logger)

	switch command {
	case "start", "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runServe(ctx, serveOptions{
			cfg:         cfg,
			manager:     manager,
			logger:      logger,
			withNetwork: command == "start",
		})
	case "stop":
		if err := manager.Stop(context.Background()); err != nil {
			logger.Error("stop failed", "error", err)
			return 1
		}
		return 0
	case "setup":
		if err := manager.FirstRunSetup(context.Background()); err != nil {
			logger.Error("setup failed", "error", err)
			return 1
		}
		// Generate the derived files too, so they can be reviewed before
		// the first start.
		if err := manager.GenerateConfigs(); err != nil {
			logger.Error("config generation failed", "error", err)
			return 1
		}
		fmt.Println("Setup complete.")
		return 0
	case "status":
		return runStatus(cfg, manager)
	case "shoutbox":
		return runShoutbox(cfg, rest)
	case "timesave":
		return runTimesave(cfg, rest)
	case "stations":
		return runStations(cfg, runner)
	case "hostname":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: luffy hostname <name>")
			return 2
		}
		if err := service.SetHostname(cfg, rest[0]); err != nil {
			logger.Error("hostname change failed", "error", err)
			return 1
		}
		fmt.Println("Switching hostname to " + rest[0])
		return 0
	case "flush-dns":
		if err := manager.FlushDNSMasq(context.Background()); err != nil {
			logger.Error("dnsmasq reload failed", "error", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usageText)
		return 2
	}
}

// serveOptions configure runServe. The context and readiness hook are
// injected so tests can drive a full server without signals.
type serveOptions struct {
	cfg         *config.Config
	manager     *service.Manager
	logger      *slog.Logger
	withNetwork bool
	// ready is called after every listener is bound. It may be nil.
	ready func(server *web.Server)
}

func runServe(ctx context.Context, opts serveOptions) int {
	// Cancelling this context stops every goroutine below. A listener error
	// must cancel it too, otherwise wg.Wait would block forever.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cfg, manager, logger := opts.cfg, opts.manager, opts.logger

	if opts.withNetwork {
		if err := manager.Start(ctx); err != nil {
			logger.Error("start failed", "error", err)
			return 1
		}
	} else {
		if err := manager.FirstRunSetup(ctx); err != nil {
			logger.Error("setup failed", "error", err)
			return 1
		}
	}

	store := shoutbox.NewStore(cfg.ChatFile)
	tracker := manager.StationsTracker()
	du := diskusage.New(cfg.UploadFolder, cfg.GenDiskFile)

	// web.New builds the forum store, content filesystem and broadcaster
	// defaults from cfg, so only the pieces with live state are passed in.
	server := web.New(web.Options{
		Cfg:       cfg,
		Logger:    logger,
		Version:   version,
		Shoutbox:  store,
		DiskUsage: du,
		Stations:  tracker,
	})

	var wg sync.WaitGroup
	errc := make(chan error, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := tracker.Run(ctx); err != nil {
			logger.Warn("station tracker stopped", "error", err)
		}
	}()

	if cfg.GlobalChat {
		listener := &shoutbox.Listener{
			Logger: logger,
			Handler: func(content string) error {
				if _, err := store.Prepend(content + "\n"); err != nil {
					return err
				}
				return store.GeneratePage(cfg.GenChatFile)
			},
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := listener.ListenAndServe(ctx); err != nil {
				logger.Error("global chat listener stopped", "error", err)
				errc <- err
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(ctx, server.ListenAddr()); err != nil {
			errc <- err
		}
	}()
	if cfg.DroopyEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := server.ListenAndServeUpload(ctx, server.UploadListenAddr()); err != nil {
				errc <- err
			}
		}()
	}

	logger.Info("luffy running",
		"version", version,
		"http", server.ListenAddr(),
		"upload", server.UploadListenAddr(),
		"host", cfg.Host)

	if opts.ready != nil {
		opts.ready(server)
	}

	var serveErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case serveErr = <-errc:
		logger.Error("server error", "error", serveErr)
	}
	cancel()
	wg.Wait()

	if opts.withNetwork {
		if err := manager.Stop(context.Background()); err != nil {
			logger.Warn("stop failed", "error", err)
		}
	}
	if serveErr != nil {
		return 1
	}
	return 0
}

func runStatus(cfg *config.Config, manager *service.Manager) int {
	fmt.Printf("luffy %s\n", version)
	fmt.Printf("config:       %s\n", cfg.Path)
	fmt.Printf("folder:       %s\n", cfg.PirateboxFolder)
	fmt.Printf("host:         %s (%s)\n", cfg.Host, cfg.IP())
	fmt.Printf("interface:    %s\n", cfg.Interface)
	fmt.Printf("http:         %s\n", fmt.Sprintf(":%d", cfg.HTTPPort))
	fmt.Printf("upload:       %s\n", fmt.Sprintf(":%d", cfg.UploadHTTPPort()))
	fmt.Printf("shoutbox:     %v\n", cfg.ShoutboxEnabled)
	fmt.Printf("global chat:  %v\n", cfg.GlobalChat)
	fmt.Printf("ipv6:         %v (%s)\n", cfg.IPv6Enable, cfg.IPv6Advert)
	fmt.Printf("dnsmasq:      %v\n", cfg.UseDNSMasq)
	fmt.Printf("hostapd:      %v\n", cfg.UseAPN)
	if _, err := os.Stat(cfg.InitDone); err == nil {
		fmt.Println("setup:        done")
	} else {
		fmt.Println("setup:        pending")
	}
	for _, err := range cfg.Validate() {
		fmt.Printf("config error: %s\n", err)
	}
	return 0
}

func runShoutbox(cfg *config.Config, args []string) int {
	store := shoutbox.NewStore(cfg.ChatFile)
	sub := "generate"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "generate":
		if err := store.GeneratePage(cfg.GenChatFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Generated HTML-Shoutbox File.")
		return 0
	case "post":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: luffy shoutbox post \"message\"")
			return 2
		}
		line := shoutbox.PrepareLine("luffy", strings.Join(args[1:], " "), "def", time.Now())
		if _, err := store.Prepend(line); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := store.GeneratePage(cfg.GenChatFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "reset":
		if err := store.Reset(""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: luffy shoutbox <generate|post|reset>")
		return 2
	}
}

func runTimesave(cfg *config.Config, args []string) int {
	saver := timesave.New(cfg.Timesave, cfg.TimesaveFormat)
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "save":
		if err := saver.Save(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "recover":
		if err := saver.Recover(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: luffy timesave <save|recover>")
		return 2
	}
}

func runStations(cfg *config.Config, runner proc.Runner) int {
	counter := &stations.IWCounter{Interface: cfg.Interface, Runner: runner}
	count, err := counter.Count(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(stations.Render(count))
	return 0
}

// dryRunRunner logs commands instead of executing them.
type dryRunRunner struct {
	logger *slog.Logger
}

func (d *dryRunRunner) Run(_ context.Context, cmd proc.Command) (string, error) {
	d.logger.Info("dry-run", "command", cmd.String())
	return "", nil
}

func (d *dryRunRunner) Start(_ context.Context, cmd proc.Command) (int, error) {
	d.logger.Info("dry-run", "command", cmd.String())
	return 0, nil
}
