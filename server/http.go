package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed web/index.html
var indexHTML []byte

//go:embed web/hls.min.js
var hlsJS []byte

type Server struct {
	cfg *Config
	lib *Library
	ff  *FFmpeg // nil when FFmpeg isn't installed
	hls *sessions

	progress *ProgressStore
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET /hls.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(hlsJS)
	})
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Write([]byte(`{"name":"Home Streamer","short_name":"Home Streamer","start_url":"/","display":"standalone","background_color":"#0f1318","theme_color":"#0f1318","icons":[{"src":"/icon.svg","sizes":"any","type":"image/svg+xml"}]}`))
	})
	mux.HandleFunc("GET /icon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 108 108"><rect width="108" height="108" rx="20" fill="#1A73E8"/><path d="M42 34L76 54L42 74Z" fill="#fff"/></svg>`))
	})
	mux.HandleFunc("GET /api/progress", s.progress.handleList)
	mux.HandleFunc("PUT /api/progress/{id}", s.progress.handlePut)
	mux.HandleFunc("DELETE /api/progress/{id}", s.progress.handleDelete)
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	mux.HandleFunc("GET /api/video/{id}", s.handleVideo)
	mux.HandleFunc("GET /stream/{id}", s.handleStream)
	mux.HandleFunc("HEAD /stream/{id}", s.handleStream)
	mux.HandleFunc("GET /subs/{id}", s.handleSubs)
	mux.HandleFunc("GET /api/media/{id}", s.handleMedia)
	mux.HandleFunc("GET /hls/{id}/index.m3u8", s.handlePlaylist)
	mux.HandleFunc("GET /hls/{id}/seg/{file}", s.handleSegment)
	mux.HandleFunc("GET /esubs/{id}/{index}", s.handleEmbeddedSub)
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
	info := map[string]any{"app": "home-streamer", "name": s.cfg.Name, "version": version, "transcode": s.ff != nil}
	if s.ff != nil {
		info["encoder"] = s.ff.Encoder
	}
	writeJSON(w, info)
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

func (s *Server) resolveVideo(w http.ResponseWriter, id string) (string, bool) {
	_, _, abs, err := s.lib.Resolve(id)
	if err == nil {
		if _, ok := videoExts[strings.ToLower(filepath.Ext(abs))]; !ok {
			err = errBadID
		} else if _, serr := os.Stat(abs); serr != nil {
			err = serr
		}
	}
	if err != nil {
		writeErr(w, err)
		return "", false
	}
	return abs, true
}

// handleMedia describes a video's streams and whether it can be converted.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	abs, ok := s.resolveVideo(w, id)
	if !ok {
		return
	}
	if s.ff == nil {
		writeJSON(w, &MediaInfo{CanTranscode: false, Audio: []AudioTrack{}, Subtitles: []EmbeddedSub{}})
		return
	}
	mi, err := s.ff.Probe(abs)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := *mi
	out.Subtitles = append([]EmbeddedSub(nil), mi.Subtitles...)
	ready := s.ff.SubsReady(abs, mi)
	out.SubsProgress = -1
	if !ready {
		out.SubsProgress = int(math.Max(0, s.ff.SubsProgress(abs)) * 100)
	}
	for i := range out.Subtitles {
		if out.Subtitles[i].Text {
			out.Subtitles[i].Ready = ready
			out.Subtitles[i].URL = fmt.Sprintf("/esubs/%s/%d", id, out.Subtitles[i].Index)
		}
	}
	writeJSON(w, &out)
}

func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	if s.ff == nil {
		http.Error(w, "conversion needs FFmpeg on the PC", http.StatusNotImplemented)
		return
	}
	abs, ok := s.resolveVideo(w, r.PathValue("id"))
	if !ok {
		return
	}
	mi, err := s.ff.Probe(abs)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(s.ff.Playlist(mi, r.URL.RawQuery)))
}

func (s *Server) handleSegment(w http.ResponseWriter, r *http.Request) {
	if s.ff == nil {
		http.Error(w, "conversion needs FFmpeg on the PC", http.StatusNotImplemented)
		return
	}
	abs, ok := s.resolveVideo(w, r.PathValue("id"))
	if !ok {
		return
	}
	n, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("file"), ".ts"))
	if err != nil || n < 0 {
		writeErr(w, errBadID)
		return
	}
	mi, err := s.ff.Probe(abs)
	if err != nil {
		writeErr(w, err)
		return
	}
	ss := s.hls.get(abs, mi, parseQuality(r.URL.Query().Get))
	p, err := s.hls.Segment(r.Context(), ss, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, p)
}

func (s *Server) handleEmbeddedSub(w http.ResponseWriter, r *http.Request) {
	if s.ff == nil {
		writeErr(w, errBadID)
		return
	}
	abs, ok := s.resolveVideo(w, r.PathValue("id"))
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeErr(w, errBadID)
		return
	}
	data, err := os.ReadFile(s.ff.SubPath(abs, idx))
	if err != nil {
		http.Error(w, "subtitles not ready yet", http.StatusNotFound)
		return
	}
	if r.URL.Query().Get("format") == "vtt" {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Write([]byte(srtToVTT(decodeText(data))))
		return
	}
	w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
	w.Write([]byte(decodeText(data)))
}
