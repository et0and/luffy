package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"luffy/internal/diskusage"
	"luffy/internal/shoutbox"
	"luffy/internal/stations"
	"luffy/internal/upload"
)

// successPage is the body served to captive portal probes, matching
// www/library/test/success.html.
const successPage = `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 3.2//EN">
<HTML>
<HEAD>
	<TITLE>Success</TITLE>
</HEAD>
<BODY>
Success
</BODY>
</HTML>
`

// redirectPage mirrors src/redirect.html.schema.
const redirectTemplate = `<html>
<head><title>Redirect...</title>
<meta http-equiv='refresh' content='0;url=http://%s/content' />
<meta http-equiv='cache-control' content='no-cache, no-store, must-revalidate' />
<meta http-equiv='pragma' content='no-cache' />
<meta http-equiv='expires' content='0' />
</head>
<body>
Redirect
</body>
</html>
`

// handleRoot serves the installed index page or the redirect page, and acts
// as the catch-all 404 handler that sends browsers to the start page.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		indexPath := filepath.Join(s.opts.Cfg.WwwFolder, "index.html")
		if info, err := os.Stat(indexPath); err == nil && info.Mode().IsRegular() {
			http.ServeFile(w, r, indexPath)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, redirectTemplate, s.hostHeader())
		return
	}
	// Anything else is the lighttpd error-handler-404 behaviour.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, redirectTemplate, s.hostHeader())
}

func (s *Server) hostHeader() string {
	host := s.opts.Cfg.Host
	if host == "" {
		host = "piratebox.lan"
	}
	if port := s.opts.Cfg.HTTPPort; port != 0 && port != 80 {
		return host + ":" + strconv.Itoa(port)
	}
	return host
}

// handleShoutboxRead serves the generated shoutbox HTML.
func (s *Server) handleShoutboxRead(w http.ResponseWriter, r *http.Request) {
	content, err := s.opts.Shoutbox.Read()
	if err != nil {
		http.Error(w, "shoutbox unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, shoutbox.GeneratePage(content))
}

// handleShoutboxWrite implements cgi-bin/psowrte.py.
func (s *Server) handleShoutboxWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.opts.Cfg.ShoutboxEnabled {
		http.Error(w, "shoutbox disabled", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("name")
	if name == "" {
		name = "&nbsp;"
	}
	data := r.PostFormValue("data")
	if data == "" {
		data = "&nbsp;"
	}
	color := r.PostFormValue("color")
	if color == "" {
		color = "def"
	}
	line := shoutbox.Line(name, data, color, r.PostFormValue("timestamp"),
		s.opts.Cfg.ShoutboxClientTime, s.opts.Now())

	if s.opts.Cfg.GlobalChat && len(s.opts.Cfg.GlobalDest) > 0 {
		// Global chat broadcasts to the network and does not store locally,
		// matching psogen.py when SHOUTBOX_BROADCAST_DESTINATIONS was set.
		// The broadcaster destinations are fixed at construction, so this is
		// safe under concurrent posts.
		msg := shoutbox.NewShoutboxMessage(s.opts.Cfg.Host)
		msg.Set(line)
		if err := s.opts.Broadcaster.Send(msg.Wire()); err != nil {
			s.opts.Logger.Warn("global chat broadcast failed", "error", err)
		}
	} else if _, err := s.opts.Shoutbox.Prepend(line); err != nil {
		s.opts.Logger.Error("shoutbox write failed", "error", err)
		http.Error(w, "could not save message", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<html><body>ok</body></html>")
}

// handleDiskUsageRead serves the generated disk usage fragment.
func (s *Server) handleDiskUsageRead(w http.ResponseWriter, r *http.Request) {
	if stale, err := s.opts.DiskUsage.Stale(); err == nil && stale {
		_ = s.opts.DiskUsage.Write()
	} else if err != nil {
		s.opts.Logger.Warn("disk usage unavailable", "error", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	data, err := os.ReadFile(s.opts.Cfg.GenDiskFile)
	if err != nil {
		// The generated file is missing or unreadable: render the frame
		// directly from the live measurement instead of a 500.
		fmt.Fprint(w, diskusage.Page(s.opts.DiskUsage.Line()))
		return
	}
	w.Write(data)
}

// handleDiskUsageWrite implements cgi-bin/diskwrite.py with the five minute
// throttle.
func (s *Server) handleDiskUsageWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.opts.DiskUsage.Refresh(false); err != nil {
		s.opts.Logger.Warn("disk usage refresh failed", "error", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<html><body>ok</body></html>")
}

// handleStationCount serves station_cnt.txt from the tracker.
func (s *Server) handleStationCount(w http.ResponseWriter, r *http.Request) {
	count := 0
	if s.opts.Stations != nil {
		count = s.opts.Stations.Count()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, stations.Render(count))
}

// handleGenerate204 fakes a working internet connection for Android.
func (s *Server) handleGenerate204(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// handleNCSI fakes a working internet connection for Windows.
func (s *Server) handleNCSI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, "Microsoft NCSI")
}

// handleSuccess serves the Apple/Android captive portal success page.
func (s *Server) handleSuccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, successPage)
}

// ConfigModules mirrors conf/json.conf.
type ConfigModules struct {
	UploadModuleFile string
	ChatModuleFile   string
}

// modulePayload matches the JSON produced by json_generation.sh.
type modulePayload struct {
	Status      bool   `json:"status"`
	File        string `json:"file"`
	UploadStyle string `json:"upload_style,omitempty"`
	DroopyPort  string `json:"droopy_port,omitempty"`
	DroopyHost  string `json:"droopy_host,omitempty"`
}

type pirateboxPayload struct {
	Piratebox struct {
		Module struct {
			Upload   modulePayload `json:"upload"`
			Shoutbox modulePayload `json:"shoutbox"`
			Version  string        `json:"version"`
		} `json:"module"`
	} `json:"piratebox"`
}

// ConfigJSON renders the frontend configuration JSON.
func (s *Server) ConfigJSON(modules ConfigModules) ([]byte, error) {
	if modules.ChatModuleFile == "" {
		modules.ChatModuleFile = "modules/chat.html"
	}
	if modules.UploadModuleFile == "" {
		modules.UploadModuleFile = "modules/upload.html"
	}
	var payload pirateboxPayload
	payload.Piratebox.Module.Upload = modulePayload{
		Status:      s.opts.Cfg.DroopyEnabled,
		File:        modules.UploadModuleFile,
		UploadStyle: "droopy",
		DroopyPort:  s.opts.Cfg.DroopyPort,
		DroopyHost:  s.opts.Cfg.Host,
	}
	payload.Piratebox.Module.Shoutbox = modulePayload{
		Status: s.opts.Cfg.ShoutboxEnabled,
		File:   modules.ChatModuleFile,
	}
	payload.Piratebox.Module.Version = s.version()
	return json.Marshal(payload)
}

func (s *Server) version() string {
	if data, err := os.ReadFile(s.opts.Cfg.VersionFile); err == nil {
		if line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]); line != "" {
			return line
		}
	}
	return s.opts.Version
}

func (s *Server) handleConfigJSON(w http.ResponseWriter, r *http.Request) {
	if !s.opts.Cfg.PbxJSONGeneration {
		// The original only generated the file when PBX_JSON_GENERATION was
		// enabled, so treat it as absent rather than serving stale content.
		http.NotFound(w, r)
		return
	}
	data, err := s.ConfigJSON(ConfigModules{})
	if err != nil {
		http.Error(w, "json generation failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// uploadHandler builds the droopy compatible handler from the config.
func (s *Server) uploadHandler() http.Handler {
	h := upload.New(s.opts.Cfg.UploadFolder)
	h.MaxBytes = s.opts.Cfg.UploadMaxBytes
	h.Message = s.opts.Cfg.DroopyTxt
	h.UserDirs = strings.TrimSpace(s.opts.Cfg.DroopyUserDir) != ""
	h.Logger = s.opts.Logger
	if mode := strings.TrimSpace(s.opts.Cfg.DroopyChmod); mode != "" {
		if parsed, err := strconv.ParseUint(mode, 8, 32); err == nil {
			h.FileMode = os.FileMode(parsed)
		}
	}
	return h
}
