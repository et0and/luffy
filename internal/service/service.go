// Package service ports init.d/piratebox and the helper scripts it called:
// network interface setup, hostapd, dnsmasq, radvd, firewall rules, the IRC
// server and the process lifecycle.
//
// The lifecycle is built from small steps that can be tested with a fake
// command runner, so the same code runs on a real router and in unit tests.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"luffy/internal/config"
	"luffy/internal/diskusage"
	"luffy/internal/hooks"
	"luffy/internal/proc"
	"luffy/internal/shoutbox"
	"luffy/internal/stations"
	"luffy/internal/sysconfig"
	"luffy/internal/timesave"
)

// Manager owns the service lifecycle.
type Manager struct {
	Cfg    *config.Config
	Runner proc.Runner
	Logger *slog.Logger
	// Now can be replaced in tests.
	Now func() time.Time

	mu      sync.Mutex
	started bool
}

// New returns a manager for cfg.
func New(cfg *config.Config, runner proc.Runner, logger *slog.Logger) *Manager {
	if runner == nil {
		runner = proc.Exec{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{Cfg: cfg, Runner: runner, Logger: logger, Now: time.Now}
}

// Start runs the startup sequence from init.d/piratebox.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return fmt.Errorf("service: already started")
	}

	m.Logger.Info("starting piratebox", "host", m.Cfg.Host, "interface", m.Cfg.Interface)

	if err := m.FirstRunSetup(ctx); err != nil {
		return err
	}
	if err := m.GenerateConfigs(); err != nil {
		return err
	}
	if err := m.runHooks(ctx, hooks.StartStages()); err != nil {
		return err
	}
	if err := m.emptyTmp(); err != nil {
		m.Logger.Warn("could not empty tmp folder", "error", err)
	}
	// The original init script cleaned aborted uploads before starting
	// droopy: zero byte files and leftover tmp* partials.
	if m.Cfg.DroopyEnabled {
		if deleted, err := DeleteEmptyFiles(m.Cfg.UploadFolder); err != nil {
			m.Logger.Warn("could not delete empty uploads", "error", err)
		} else if deleted > 0 {
			m.Logger.Info("deleted empty uploads", "count", deleted)
		}
		if removed, err := RemovePartialUploads(m.Cfg.UploadFolder); err != nil {
			m.Logger.Warn("could not remove partial uploads", "error", err)
		} else if removed > 0 {
			m.Logger.Info("removed partial uploads", "count", removed)
		}
	}
	if m.Cfg.CustomDirlistCopy {
		sourceDir := filepath.Join(m.Cfg.PirateboxFolder, "src")
		if err := DistributeFiles(m.Cfg.UploadFolder, sourceDir, false); err != nil {
			m.Logger.Warn("could not distribute directory listing files", "error", err)
		}
	}
	if err := m.setupInterface(ctx); err != nil {
		return err
	}
	if err := m.startHostapd(ctx); err != nil {
		return err
	}
	if err := m.bridgeInterface(ctx); err != nil {
		return err
	}
	if err := m.startDNSMasq(ctx); err != nil {
		return err
	}
	if err := m.firewall(ctx, true); err != nil {
		m.Logger.Warn("firewall rules failed", "error", err)
	}
	if err := m.startRadvd(ctx); err != nil {
		m.Logger.Warn("radvd failed", "error", err)
	}
	if err := m.startIRC(ctx); err != nil {
		m.Logger.Warn("IRC server failed", "error", err)
	}
	if err := m.prepareShoutbox(); err != nil {
		m.Logger.Warn("shoutbox preparation failed", "error", err)
	}
	if err := m.runHooks(ctx, hooks.StartDoneStages()); err != nil {
		return err
	}

	m.started = true
	m.Logger.Info("piratebox started")
	return nil
}

// Stop runs the shutdown sequence from init.d/piratebox.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Logger.Info("stopping piratebox")
	if err := m.runHooks(ctx, hooks.StopStages()); err != nil {
		m.Logger.Warn("stop hook failed", "error", err)
	}
	if m.Cfg.EnableIRCServer {
		m.stopProcess(ctx, "miniircd.py", "irc")
	}
	if err := m.firewall(ctx, false); err != nil {
		m.Logger.Warn("removing firewall rules failed", "error", err)
	}
	if m.Cfg.UseAPN {
		m.stopProcess(ctx, "hostapd", "hostapd")
	}
	if m.Cfg.UseDNSMasq {
		m.stopProcess(ctx, "dnsmasq", "dnsmasq")
	}
	if m.Cfg.IPv6Enable && m.Cfg.IPv6Advert == "radvd" {
		m.stopProcess(ctx, "radvd", "radvd")
	}
	if err := m.stopInterface(ctx); err != nil {
		m.Logger.Warn("stopping interface failed", "error", err)
	}
	if err := m.runHooks(ctx, hooks.StopDoneStages()); err != nil {
		m.Logger.Warn("stop done hook failed", "error", err)
	}
	m.started = false
	m.Logger.Info("piratebox stopped")
	return nil
}

// FirstRunSetup performs the one time installation steps that the original
// init script ran when conf/init_done was missing.
func (m *Manager) FirstRunSetup(ctx context.Context) error {
	if _, err := os.Stat(m.Cfg.InitDone); err == nil {
		return nil
	}
	m.Logger.Info("first start: running setup")
	if err := m.runHooks(ctx, hooks.InitStages()); err != nil {
		return err
	}
	if err := m.CreateContentFolder(); err != nil {
		return err
	}
	if err := m.CreatePart2Layout(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Cfg.InitDone), 0o755); err != nil {
		return fmt.Errorf("service: create conf dir: %w", err)
	}
	return os.WriteFile(m.Cfg.InitDone, []byte(m.Now().Format(time.RFC3339)+"\n"), 0o644)
}

// CreateContentFolder ports the create_content_folder function from
// install_piratebox.sh: copy the shipped web content to the share folder and
// symlink it into the web root.
func (m *Manager) CreateContentFolder() error {
	if _, err := os.Stat(m.Cfg.WwwContent); err == nil {
		return nil
	}
	m.Logger.Info("creating content folder", "path", m.Cfg.WwwContent)
	if err := os.MkdirAll(m.Cfg.WwwContent, 0o755); err != nil {
		return fmt.Errorf("service: create content folder: %w", err)
	}
	if err := CopyAssets(m.Cfg.WwwContent); err != nil {
		return err
	}
	return m.linkContent()
}

// linkContent creates the www/content symlink and favicon link.
func (m *Manager) linkContent() error {
	link := filepath.Join(m.Cfg.WwwFolder, "content")
	if _, err := os.Lstat(link); err != nil {
		if err := os.MkdirAll(m.Cfg.WwwFolder, 0o755); err != nil {
			return fmt.Errorf("service: create www folder: %w", err)
		}
		if err := os.Symlink(m.Cfg.WwwContent, link); err != nil {
			return fmt.Errorf("service: symlink content: %w", err)
		}
	}
	favicon := filepath.Join(m.Cfg.WwwFolder, "favicon.ico")
	if _, err := os.Lstat(favicon); err != nil {
		src := filepath.Join(m.Cfg.WwwContent, "favicon.ico")
		if _, err := os.Stat(src); err == nil {
			if err := os.Symlink(src, favicon); err != nil {
				return fmt.Errorf("service: symlink favicon: %w", err)
			}
		}
	}
	return nil
}

// CreatePart2Layout ports the part2 branch of install_piratebox.sh: create
// the share folders and the www symlinks.
func (m *Manager) CreatePart2Layout() error {
	for _, dir := range []string{
		m.Cfg.UploadFolder,
		filepath.Join(m.Cfg.ShareFolder, "board"),
		filepath.Join(m.Cfg.ShareFolder, "tmp"),
		filepath.Join(m.Cfg.PirateboxFolder, "tmp"),
		filepath.Join(m.Cfg.ShareFolder, "forumspace"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("service: create %s: %w", dir, err)
		}
	}
	links := map[string]string{
		filepath.Join(m.Cfg.WwwFolder, "Shared"):  m.Cfg.UploadFolder,
		filepath.Join(m.Cfg.WwwFolder, "board"):   filepath.Join(m.Cfg.ShareFolder, "board"),
		filepath.Join(m.Cfg.WwwFolder, "content"): m.Cfg.WwwContent,
	}
	for link, target := range links {
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return fmt.Errorf("service: create www dir: %w", err)
		}
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("service: symlink %s: %w", link, err)
		}
	}
	return nil
}

// GenerateConfigs writes hosts, dnsmasq, radvd, lighttpd env and avahi
// files. Start calls this automatically; the setup command exposes it so an
// operator can inspect the generated files before the first start.
func (m *Manager) GenerateConfigs() error {
	generator := sysconfig.New(m.Cfg)
	if err := generator.Generate(); err != nil {
		return err
	}
	m.Logger.Info("generated configuration files")
	return nil
}

// emptyTmp removes files from the tmp folder, matching `find tmp -exec rm`.
func (m *Manager) emptyTmp() error {
	tmpDir := filepath.Join(m.Cfg.PirateboxFolder, "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(tmpDir, entry.Name())); err != nil {
			return fmt.Errorf("service: empty tmp: %w", err)
		}
	}
	return nil
}

// setupInterface ports piratebox_setup_wlan.sh start/probe.
func (m *Manager) setupInterface(ctx context.Context) error {
	if m.Cfg.DoIW {
		if _, err := m.Runner.Run(ctx, proc.Command{
			Name: "iw",
			Args: []string{m.Cfg.PhyIf, "interface", "add", m.Cfg.Interface, "type", "managed"},
		}); err != nil {
			return fmt.Errorf("service: iw interface add: %w", err)
		}
	}
	if m.Cfg.ProbeInterface {
		if err := m.probeInterface(ctx); err != nil {
			return err
		}
	}
	if !m.Cfg.DoIfconfig {
		return nil
	}
	if _, err := m.Runner.Run(ctx, proc.Command{Name: "ifconfig", Args: []string{m.Cfg.Interface, "up"}}); err != nil {
		return fmt.Errorf("service: bring up %s: %w", m.Cfg.Interface, err)
	}
	if _, err := m.Runner.Run(ctx, proc.Command{
		Name: "ifconfig",
		Args: []string{m.Cfg.Interface, m.Cfg.IP(), "netmask", m.Cfg.Netmask},
	}); err != nil {
		return fmt.Errorf("service: set address on %s: %w", m.Cfg.Interface, err)
	}
	if m.Cfg.IPv6Enable {
		address := m.Cfg.IPv6IP + "/" + m.Cfg.IPv6Mask
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "ifconfig", Args: []string{m.Cfg.Interface, "add", address}}); err != nil {
			return fmt.Errorf("service: add ipv6 address: %w", err)
		}
		if _, err := m.Runner.Run(ctx, proc.Command{
			Name: "ip",
			Args: []string{"addr", "change", address, "dev", m.Cfg.Interface, "scope", "link"},
		}); err != nil {
			return fmt.Errorf("service: set ipv6 scope: %w", err)
		}
	}
	if m.Cfg.NodeConfigActive && m.Cfg.NodeIPv6SetIP && m.Cfg.NodeInterface != "" {
		address := m.Cfg.NodeIPv6IP + m.Cfg.NodeIPv6Mask
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "ifconfig", Args: []string{m.Cfg.NodeInterface, "add", address}}); err != nil {
			return fmt.Errorf("service: add node address: %w", err)
		}
	}
	return nil
}

// probeInterface waits until the interface exists, matching the probe loop.
func (m *Manager) probeInterface(ctx context.Context) error {
	attempts := m.Cfg.ProbeTime
	if attempts <= 0 {
		attempts = 10
	}
	for i := 0; i < attempts; i++ {
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "ifconfig", Args: []string{m.Cfg.Interface}}); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("service: interface %s did not appear within %ds", m.Cfg.Interface, attempts)
}

// stopInterface ports piratebox_setup_wlan.sh stop.
func (m *Manager) stopInterface(ctx context.Context) error {
	if m.Cfg.NodeConfigActive && m.Cfg.NodeIPv6SetIP && m.Cfg.NodeInterface != "" {
		address := m.Cfg.NodeIPv6IP + m.Cfg.NodeIPv6Mask
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "ifconfig", Args: []string{m.Cfg.NodeInterface, "del", address}}); err != nil {
			m.Logger.Warn("could not remove node address", "error", err)
		}
	}
	if m.Cfg.DoIfconfig {
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "ifconfig", Args: []string{m.Cfg.Interface, "down"}}); err != nil {
			return fmt.Errorf("service: bring down %s: %w", m.Cfg.Interface, err)
		}
	}
	if m.Cfg.DoIW {
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "iw", Args: []string{"dev", m.Cfg.Interface, "del"}}); err != nil {
			m.Logger.Warn("could not delete iw interface", "error", err)
		}
	}
	return nil
}

// bridgeInterface ports the brctl addif call.
func (m *Manager) bridgeInterface(ctx context.Context) error {
	if !m.Cfg.DoBridge {
		return nil
	}
	if _, err := m.Runner.Run(ctx, proc.Command{
		Name: "brctl",
		Args: []string{"addif", m.Cfg.Bridge, m.Cfg.Interface},
	}); err != nil {
		return fmt.Errorf("service: bridge interface: %w", err)
	}
	return nil
}

// startHostapd launches hostapd with the generated configuration.
func (m *Manager) startHostapd(ctx context.Context) error {
	if !m.Cfg.UseAPN {
		return nil
	}
	conf := filepath.Join(m.Cfg.PirateboxFolder, "conf", "hostapd.conf")
	pid, err := m.Runner.Start(ctx, proc.Command{
		Name: "hostapd",
		Args: []string{conf},
		Env:  append(os.Environ(), "HOSTAPD_PIDFILE="+m.pidFile("hostapd")),
	})
	if err != nil {
		return fmt.Errorf("service: start hostapd: %w", err)
	}
	m.Logger.Info("started hostapd", "pid", pid)
	return nil
}

// startDNSMasq launches dnsmasq with the generated configuration.
func (m *Manager) startDNSMasq(ctx context.Context) error {
	if !m.Cfg.UseDNSMasq {
		return nil
	}
	pid, err := m.Runner.Start(ctx, proc.Command{
		Name: "dnsmasq",
		Args: []string{"-x", m.pidFile("dnsmasq"), "-C", m.Cfg.DNSMasqConf},
	})
	if err != nil {
		return fmt.Errorf("service: start dnsmasq: %w", err)
	}
	m.Logger.Info("started dnsmasq", "pid", pid)
	return nil
}

// startRadvd launches radvd when IPv6 advertising is enabled.
func (m *Manager) startRadvd(ctx context.Context) error {
	if !m.Cfg.IPv6Enable || m.Cfg.IPv6Advert != "radvd" {
		return nil
	}
	pid, err := m.Runner.Start(ctx, proc.Command{
		Name: "radvd",
		Args: []string{"-p", m.pidFile("radvd"), "-C", m.Cfg.RadvdConf},
	})
	if err != nil {
		return fmt.Errorf("service: start radvd: %w", err)
	}
	m.Logger.Info("started radvd", "pid", pid)
	return nil
}

// firewall applies or removes the iptables rules from firewall.sh.
func (m *Manager) firewall(ctx context.Context, add bool) error {
	flag := "-A"
	if !add {
		flag = "-D"
	}
	ip := m.Cfg.IP()
	var errs []string
	run := func(args ...string) {
		if _, err := m.Runner.Run(ctx, proc.Command{Name: "iptables", Args: args}); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if m.Cfg.FirewallFetchDNS {
		run("-t", "nat", flag, "PREROUTING", "-i", m.Cfg.DNSMasqInterface, "-d", "0/0",
			"-p", "tcp", "--dport", "53", "-j", "DNAT", "--to-destination", ip+":53")
		run("-t", "nat", flag, "PREROUTING", "-i", m.Cfg.DNSMasqInterface, "-d", "0/0",
			"-p", "udp", "--dport", "53", "-j", "DNAT", "--to-destination", ip+":53")
	}
	if m.Cfg.FirewallFetchHTTP {
		run("-t", "nat", flag, "PREROUTING", "-i", m.Cfg.DNSMasqInterface, "-d", "0/0",
			"-p", "tcp", "--dport", "80", "-j", "DNAT", "--to-destination", ip+":80")
	}
	if len(errs) > 0 {
		return fmt.Errorf("service: iptables: %s", strings.Join(errs, "; "))
	}
	return nil
}

// prepareShoutbox ports shoutbox_stuff.sh: reset the chat if requested,
// regenerate the HTML frame and the disk usage page.
func (m *Manager) prepareShoutbox() error {
	store := shoutbox.NewStore(m.Cfg.ChatFile)
	if m.Cfg.ResetChat {
		initial := ""
		if data, err := os.ReadFile(filepath.Join(m.Cfg.PirateboxFolder, "conf", "chat_init.txt")); err == nil {
			initial = string(data)
		}
		if err := store.Reset(initial); err != nil {
			return err
		}
	}
	if err := store.GeneratePage(m.Cfg.GenChatFile); err != nil {
		return err
	}
	du := diskusage.New(m.Cfg.UploadFolder, m.Cfg.GenDiskFile)
	if err := du.Write(); err != nil {
		return err
	}
	if !m.Cfg.ShoutboxEnabled {
		if err := os.Chmod(m.Cfg.ChatFile, 0o444); err != nil {
			m.Logger.Warn("could not make shoutbox read only", "error", err)
		}
	}
	return nil
}

// StationsTracker builds the station counter used by the web server.
func (m *Manager) StationsTracker() *stations.Tracker {
	counter := &stations.IWCounter{Interface: m.Cfg.Interface, Runner: m.Runner}
	return stations.NewTracker(counter, filepath.Join(m.Cfg.WwwFolder, "station_cnt.txt"), 2*time.Minute)
}

// Timesave returns a timesave helper for the configured paths.
func (m *Manager) Timesave() *timesave.Saver {
	return timesave.New(m.Cfg.Timesave, m.Cfg.TimesaveFormat)
}

// Hooks returns a hook runner for the installation. The timeout keeps a
// stuck user hook from blocking start and stop forever.
func (m *Manager) Hooks() *hooks.Runner {
	runner := hooks.New(filepath.Join(m.Cfg.PirateboxFolder, "bin", "hooks"), m.Cfg.Path)
	runner.Runner = m.Runner
	runner.Logger = m.Logger
	return runner
}

func (m *Manager) runHooks(ctx context.Context, stages []string) error {
	runner := m.Hooks()
	for _, stage := range stages {
		if err := runner.Run(ctx, stage); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) pidFile(name string) string {
	return filepath.Join(m.Cfg.PirateboxFolder, "tmp", name+".pid")
}

// stopProcess terminates a background process using its pid file, falling
// back to pkill like the original start-stop-daemon calls.
func (m *Manager) stopProcess(ctx context.Context, processName, pidName string) {
	pidPath := m.pidFile(pidName)
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
			if _, err := m.Runner.Run(ctx, proc.Command{Name: "kill", Args: []string{strconv.Itoa(pid)}}); err != nil {
				m.Logger.Warn("could not stop process", "process", processName, "pid", pid, "error", err)
			}
			_ = os.Remove(pidPath)
			return
		}
	}
	if _, err := m.Runner.Run(ctx, proc.Command{Name: "pkill", Args: []string{"-f", processName}}); err != nil {
		m.Logger.Debug("pkill found no process", "process", processName)
	}
}
