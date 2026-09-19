// Package dirlist ports lighttpd's directory listing for the /content and
// /Shared folders, including the custom HEADER.txt and README.txt support
// configured by PirateBox.
package dirlist

import (
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// IndexNames matches the lighttpd index-file.names setting.
var IndexNames = []string{"index.php", "index.html", "index.htm", "default.htm", "index.lighttpd.html"}

// extraTypes fills gaps in Go's mime table, from conf/lighttpd/mime.types.
var extraTypes = map[string]string{
	".gz":      "application/x-gzip",
	".tgz":     "application/x-tgz",
	".tar.gz":  "application/x-tgz",
	".torrent": "application/x-bittorrent",
	".mp3":     "audio/mpeg",
	".m3u":     "audio/x-mpegurl",
	".ogg":     "application/ogg",
	".wav":     "audio/x-wav",
	".wma":     "audio/x-ms-wma",
	".avi":     "video/x-msvideo",
	".mov":     "video/quicktime",
	".asf":     "video/x-ms-asf",
	".log":     "text/plain",
	".conf":    "text/plain",
	".asc":     "text/plain",
}

// Handler serves a directory tree with listings.
type Handler struct {
	Root       string
	Prefix     string
	CSSPath    string
	ShowHeader bool
	ShowReadme bool
}

// New returns a handler rooted at root and mounted at prefix.
func New(root, prefix string) *Handler {
	return &Handler{
		Root:       root,
		Prefix:     strings.TrimSuffix(prefix, "/") + "/",
		CSSPath:    "/content/css/page_style.css",
		ShowHeader: true,
		ShowReadme: true,
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel, ok := h.relativePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	fullPath := filepath.Join(h.Root, filepath.FromSlash(rel))
	info, err := os.Stat(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		for _, index := range IndexNames {
			candidate := filepath.Join(fullPath, index)
			if fi, err := os.Stat(candidate); err == nil && fi.Mode().IsRegular() {
				serveFile(w, r, candidate)
				return
			}
		}
		h.renderListing(w, r, fullPath, rel)
		return
	}
	serveFile(w, r, fullPath)
}

// relativePath turns a request path into a slash separated path relative to
// the mounted prefix. It rejects anything that would escape the root.
func (h *Handler) relativePath(requestPath string) (string, bool) {
	if !strings.HasPrefix(requestPath, h.Prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(requestPath, h.Prefix)
	cleaned := path.Clean("/" + rest)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		cleaned = ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func (h *Handler) renderListing(w http.ResponseWriter, r *http.Request, dir, rel string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	var b strings.Builder
	prefix := h.Prefix
	if prefix == "" {
		prefix = "/"
	}
	displayURL := path.Join(prefix, rel)
	if !strings.HasSuffix(displayURL, "/") {
		displayURL += "/"
	}
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	b.WriteString("<!DOCTYPE html PUBLIC \"-//W3C//DTD XHTML 1.0 Transitional//EN\" " +
		"\"http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd\">\n")
	fmt.Fprintf(&b, "<html xmlns=\"http://www.w3.org/1999/xhtml\" xml:lang=\"en\" lang=\"en\">\n")
	fmt.Fprintf(&b, "<head>\n  <meta http-equiv=\"Content-Type\" content=\"text/html; charset=utf-8\" />\n")
	fmt.Fprintf(&b, "  <title>Index of %s</title>\n", html.EscapeString(displayURL))
	if h.CSSPath != "" {
		fmt.Fprintf(&b, "  <link rel=\"stylesheet\" href=\"%s\" type=\"text/css\" />\n", html.EscapeString(h.CSSPath))
	}
	b.WriteString("</head>\n<body>\n")

	if h.ShowHeader {
		if data, err := os.ReadFile(filepath.Join(dir, "HEADER.txt")); err == nil {
			b.Write(data)
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "<h1>Index of %s</h1>\n", html.EscapeString(displayURL))
	b.WriteString("<div class=\"directory-listing\">\n")
	b.WriteString("<table summary=\"Directory Listing\" cellpadding=\"0\" cellspacing=\"0\">\n")
	b.WriteString("<thead><tr><th class=\"n\">Name</th><th class=\"m\">Last Modified</th>" +
		"<th class=\"s\">Size</th><th class=\"t\">Type</th></tr></thead>\n")
	b.WriteString("<tbody>\n")
	if rel != "" {
		b.WriteString("<tr><td class=\"n\"><a href=\"../\">Parent Directory</a>/</td>" +
			"<td class=\"m\">&nbsp;</td><td class=\"s\">- &nbsp;</td><td class=\"t\">Directory</td></tr>\n")
	}
	row := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if (name == "HEADER.txt" && h.ShowHeader) || (name == "README.txt" && h.ShowReadme) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		className := ""
		if row%2 == 1 {
			className = " class=\"alt\""
		}
		row++
		href := (&url.URL{Path: name}).EscapedPath()
		if entry.IsDir() {
			href += "/"
		}
		fmt.Fprintf(&b, "<tr%s><td class=\"n\"><a href=\"%s\">%s</a>%s</td>"+
			"<td class=\"m\">%s</td><td class=\"s\">%s</td><td class=\"t\">%s</td></tr>\n",
			className, href, html.EscapeString(name), dirSuffix(entry.IsDir()),
			info.ModTime().Format("2006-01-02 15:04:05"), sizeLabel(entry.IsDir(), info.Size()),
			html.EscapeString(typeLabel(entry.IsDir(), name)))
	}
	b.WriteString("</tbody>\n</table>\n</div>\n")

	if h.ShowReadme {
		if data, err := os.ReadFile(filepath.Join(dir, "README.txt")); err == nil {
			b.Write(data)
			b.WriteString("\n")
		}
	}
	b.WriteString("</body>\n</html>\n")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=60, must-revalidate, no-store, no-cache, public")
	fmt.Fprint(w, b.String())
}

func dirSuffix(isDir bool) string {
	if isDir {
		return "/"
	}
	return ""
}

func sizeLabel(isDir bool, size int64) string {
	if isDir {
		return "- &nbsp;"
	}
	switch {
	case size < 1024:
		return fmt.Sprintf("%dB", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(size)/1024)
	case size < 1024*1024*1024:
		return fmt.Sprintf("%.1fM", float64(size)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fG", float64(size)/(1024*1024*1024))
	}
}

func typeLabel(isDir bool, name string) string {
	if isDir {
		return "Directory"
	}
	return ContentType(name)
}

// ContentType resolves a file name to a MIME type using Go's table and the
// PirateBox overrides.
func ContentType(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range []string{".tar.gz", ".tar.bz2"} {
		if strings.HasSuffix(lower, ext) {
			if custom, ok := extraTypes[ext]; ok {
				return custom
			}
		}
	}
	if custom, ok := extraTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return custom
	}
	if detected := mime.TypeByExtension(filepath.Ext(name)); detected != "" {
		return detected
	}
	return "application/octet-stream"
}

func serveFile(w http.ResponseWriter, r *http.Request, fullPath string) {
	f, err := os.Open(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ContentType(info.Name()))
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
