// Package web assembles the HTTP surface that replaces lighttpd, the
// cgi-bin scripts and droopy.
package web

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"luffy/assets"
	"luffy/internal/config"
	"luffy/internal/dirlist"
	"luffy/internal/diskusage"
	"luffy/internal/forum"
	"luffy/internal/shoutbox"
	"luffy/internal/stations"
)

// Options configure a Server. Nil fields get sensible defaults built from
// Cfg so tests can override individual pieces.
type Options struct {
	Cfg         *config.Config
	Logger      *slog.Logger
	Version     string
	Shoutbox    *shoutbox.Store
	Forum       *forum.Store
	DiskUsage   *diskusage.Generator
	Stations    *stations.Tracker
	Broadcaster *shoutbox.Broadcaster
	Now         func() time.Time
	// ContentFS serves /content when the on-disk content folder is missing.
	ContentFS http.FileSystem
}

// Server wires the handlers together.
type Server struct {
	opts Options
}

// New returns a Server.
func New(opts Options) *Server {
	if opts.Cfg == nil {
		opts.Cfg = config.FromValues(config.Values{})
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Shoutbox == nil {
		opts.Shoutbox = shoutbox.NewStore(opts.Cfg.ChatFile)
	}
	if opts.Forum == nil {
		cfg := forum.Defaults(filepath.Join(opts.Cfg.ShareFolder, "forumspace"))
		cfg.CGIURL = "/board/"
		if opts.Cfg.Host != "" {
			cfg.BoardDescription = "PirateBox Board. Put media reviews or questions here.<br>\n" +
				`<A HREF="http://` + opts.Cfg.Host + `">Click here to go back to the main site</a> `
		}
		opts.Forum = forum.New(cfg)
	}
	if opts.DiskUsage == nil {
		opts.DiskUsage = diskusage.New(opts.Cfg.UploadFolder, opts.Cfg.GenDiskFile)
	}
	if opts.ContentFS == nil {
		opts.ContentFS = http.FS(assets.WWWContent())
	}
	return &Server{opts: opts}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	content := dirlist.New(s.opts.Cfg.WwwContent, "/content")
	content.CSSPath = "/content/css/page_style.css"
	shared := dirlist.New(s.opts.Cfg.UploadFolder, "/Shared")

	mux.HandleFunc("/cgi-bin/psowrte.py", s.handleShoutboxWrite)
	mux.HandleFunc("/cgi-bin/psoread.py", s.handleShoutboxRead)
	mux.HandleFunc("/cgi-bin/diskwrite.py", s.handleDiskUsageWrite)
	mux.Handle("/cgi-bin/forest.py", http.StripPrefix("/cgi-bin", forum.NewHandler(s.opts.Forum)))

	mux.Handle("/board/", forum.NewHandler(s.opts.Forum))
	mux.Handle("/board", http.RedirectHandler("/board/", http.StatusMovedPermanently))

	mux.HandleFunc("/chat_content.html", s.handleShoutboxRead)
	mux.HandleFunc("/diskusage.html", s.handleDiskUsageRead)
	mux.HandleFunc("/station_cnt.txt", s.handleStationCount)
	mux.HandleFunc("/piratebox_config.json", s.handleConfigJSON)

	mux.HandleFunc("/generate_204", s.handleGenerate204)
	mux.HandleFunc("/ncsi.txt", s.handleNCSI)
	mux.HandleFunc("/hotspot-detect.html", s.handleSuccess)
	mux.HandleFunc("/library/test/success.html", s.handleSuccess)

	mux.Handle("/Shared/", shared)
	mux.Handle("/Shared", http.RedirectHandler("/Shared/", http.StatusMovedPermanently))
	mux.HandleFunc("/content/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(s.opts.Cfg.WwwContent); err != nil {
			http.StripPrefix("/content/", http.FileServer(s.opts.ContentFS)).ServeHTTP(w, r)
			return
		}
		content.ServeHTTP(w, r)
	})
	mux.Handle("/content", http.RedirectHandler("/content/", http.StatusMovedPermanently))

	// Root and catch-all, matching lighttpd's error-handler-404 = index.html
	// captive portal redirect.
	mux.HandleFunc("/", s.handleRoot)

	return s.captiveMiddleware(mux)
}

// captiveMiddleware makes every URL look like a working internet connection
// for iOS captive portal detection, like the lighttpd user agent rule did.
func (s *Server) captiveMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isCaptiveAgent(r.UserAgent()) {
			s.handleSuccess(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isCaptiveAgent(userAgent string) bool {
	return strings.Contains(strings.ToLower(userAgent), "captivenetworksupport")
}

// ListenAddr returns the configured main listen address.
func (s *Server) ListenAddr() string {
	return net.JoinHostPort(s.opts.Cfg.HTTPAddr, strconv.Itoa(s.opts.Cfg.HTTPPort))
}

// UploadListenAddr returns the droopy compatible listen address.
func (s *Server) UploadListenAddr() string {
	return net.JoinHostPort(s.opts.Cfg.HTTPAddr, strconv.Itoa(s.opts.Cfg.UploadHTTPPort()))
}

// ListenAndServe runs the main server until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	return s.serve(ctx, addr, s.Handler(), "http")
}

// ListenAndServeUpload runs the droopy compatible upload server.
func (s *Server) ListenAndServeUpload(ctx context.Context, addr string) error {
	return s.serve(ctx, addr, s.uploadHandler(), "upload")
}

func (s *Server) serve(ctx context.Context, addr string, handler http.Handler, name string) error {
	if addr == "" {
		addr = s.ListenAddr()
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	s.opts.Logger.Info("listening", "service", name, "addr", addr)
	err := server.ListenAndServe()
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
