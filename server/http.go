package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed web/index.html
var indexHTML []byte

type Server struct {
	cfg *Config
	lib *Library
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	mux.HandleFunc("GET /api/video/{id}", s.handleVideo)
	mux.HandleFunc("GET /stream/{id}", s.handleStream)
	mux.HandleFunc("HEAD /stream/{id}", s.handleStream)
	mux.HandleFunc("GET /subs/{id}", s.handleSubs)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadID), errors.Is(err, fs.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, fs.ErrPermission):
		http.Error(w, "permission denied", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"app": "movies-app", "name": s.cfg.Name, "version": version})
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	l, err := s.lib.Browse(r.URL.Query().Get("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, l)
}

func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	v, err := s.lib.Video(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, v)
}

// handleStream serves the raw file with HTTP range support, so the player can seek.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	_, _, abs, err := s.lib.Resolve(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	mime, ok := videoExts[strings.ToLower(filepath.Ext(abs))]
	if !ok {
		writeErr(w, errBadID)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeErr(w, errBadID)
		return
	}
	w.Header().Set("Content-Type", mime)
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleSubs serves a sidecar subtitle as UTF-8 SRT, or WebVTT with ?format=vtt.
func (s *Server) handleSubs(w http.ResponseWriter, r *http.Request) {
	_, _, abs, err := s.lib.Resolve(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if !subExts[strings.ToLower(filepath.Ext(abs))] {
		writeErr(w, errBadID)
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		writeErr(w, err)
		return
	}
	if st.Size() > 20<<20 {
		http.Error(w, "subtitle file too large", http.StatusRequestEntityTooLarge)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		writeErr(w, err)
		return
	}
	text := decodeText(data)
	if r.URL.Query().Get("format") == "vtt" {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Write([]byte(srtToVTT(text)))
		return
	}
	w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
	w.Write([]byte(text))
}
