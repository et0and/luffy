package forum

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// Handler serves the board over HTTP. It replaces the forest.py CGI script.
type Handler struct {
	Store *Store
}

// NewHandler returns a handler for store.
func NewHandler(store *Store) *Handler { return &Handler{Store: store} }

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	postError := ""

	if query.Get("new") != "" {
		if r.Method != http.MethodPost {
			h.renderError(w, r, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		if err := r.ParseForm(); err != nil {
			h.renderError(w, r, http.StatusBadRequest, "Bad Request")
			return
		}
		switch query.Get("new") {
		case "thread":
			hash, err := h.Store.NewThread(formValue(r, "author", h.Store.Config().AnonAuthor), r.PostFormValue("subject"), r.PostFormValue("body"))
			if err == nil {
				h.redirect(w, hash, 0)
				return
			}
			postError = err.Error()
		case "reply":
			err := h.Store.Reply(query.Get("thread"), formValue(r, "author", h.Store.Config().AnonAuthor), r.PostFormValue("body"))
			if err == nil {
				h.redirect(w, query.Get("thread"), 0)
				return
			}
			postError = err.Error()
		default:
			postError = ErrInvalidThread.Error()
		}
	}

	offset := parseOffset(query.Get("offset"))

	if threadHash := query.Get("thread"); threadHash != "" {
		thread, err := h.Store.Thread(threadHash, offset)
		if err != nil {
			status := http.StatusNotFound
			if errors.Is(err, ErrInvalidThread) {
				status = http.StatusOK
			}
			h.renderError(w, r, status, ErrInvalidThread.Error())
			return
		}
		writeHTML(w, http.StatusOK, h.Store.RenderThread(thread, offset))
		return
	}

	summaries, total, err := h.Store.ListThreads(offset)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if postError != "" {
		writeHTML(w, http.StatusOK, h.Store.RenderErrorPage(postError, summaries, total, offset))
		return
	}
	writeHTML(w, http.StatusOK, h.Store.RenderIndex(summaries, total, offset))
}

func (h *Handler) redirect(w http.ResponseWriter, hash string, offset int) {
	location := "?thread=" + hash
	if offset > 0 {
		location += "&offset=" + strconv.Itoa(offset)
	}
	w.Header().Set("Location", location)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusSeeOther)
	fmt.Fprint(w, h.Store.RenderRedirect(hash, offset))
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	writeHTML(w, status, h.Store.RenderStandaloneError(message))
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=60, must-revalidate, no-store, no-cache, public")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

func formValue(r *http.Request, key, fallback string) string {
	if v := r.PostFormValue(key); v != "" {
		return v
	}
	return fallback
}

func parseOffset(raw string) int {
	if raw == "" {
		return 0
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}
