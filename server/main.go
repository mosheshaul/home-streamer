// Home Streamer server: shares video folders on the home network so the
// Android TV app can browse and stream them, including subtitles.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var version = "dev"

// Config is stored in config.json next to the executable.
type Config struct {
	Name    string   `json:"name"`
	Port    int      `json:"port"`
	Folders []string `json:"folders"`
	// Optional: path to ffmpeg.exe, H.264 encoder (h264_nvenc, h264_qsv,
	// h264_amf or libx264) and decoder acceleration ("auto" or "none").
	FFmpeg  string `json:"ffmpeg,omitempty"`
	Encoder string `json:"encoder,omitempty"`
	HWAccel string `json:"hwaccel,omitempty"`
	// Optional: a free TMDB (themoviedb.org) key, used to show Israeli
	// films and shows by their Hebrew names. Can be set in the app or the web player.
	TMDBKey string `json:"tmdbKey,omitempty"`
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var (
		configPath = flag.String("config", "", "path to config.json (default: next to the program)")
		port       = flag.Int("port", 0, "HTTP port (overrides config)")
		folders    multiFlag
	)
	flag.Var(&folders, "folder", "video folder to share (repeatable, overrides config)")
	flag.Parse()

	if *configPath == "" {
		*configPath = defaultConfigPath()
	}
	setupLogging(filepath.Dir(*configPath))
	if !singleInstance() {
		return
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal("config: %v", err)
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if len(folders) > 0 {
		cfg.Folders = folders
	}

	lib := NewLibrary(cfg.Folders)
	lib.Titles = NewTitles(filepath.Join(filepath.Dir(*configPath), "titles.json"), cfg.TMDBKey)
	if !lib.Titles.Enabled() {
		log.Printf("Hebrew titles are off: add a TMDB key in the web player (Settings → Hebrew titles).")
	}
	if len(lib.Roots) == 0 {
		log.Printf("WARNING: no usable video folders. Edit %s and restart.", *configPath)
	}
	for _, r := range lib.Roots {
		log.Printf("Sharing folder: %s", r.Path)
	}

	srv := &Server{cfg: cfg, cfgPath: *configPath, lib: lib, progress: NewProgressStore(filepath.Join(filepath.Dir(*configPath), "progress.json"))}
	srv.progress.nameOf = func(id string) (string, bool) {
		_, rel, _, err := lib.Resolve(id)
		if err != nil {
			return "", false
		}
		base := path.Base(rel)
		return lib.display(strings.TrimSuffix(base, path.Ext(base))), true
	}
	if ff := findFFmpeg(cfg); ff != nil {
		srv.ff = ff
		srv.hls = newSessions(ff)
		log.Printf("FFmpeg found (%s, encoder %s): videos can be converted for devices that can't play them.", ff.FFmpeg, ff.Encoder)
	} else {
		log.Printf("FFmpeg not found: conversion is off. Install it with: winget install Gyan.FFmpeg  (then restart HomeStreamer)")
	}
	go func() {
		if err := runDiscovery(cfg); err != nil {
			log.Printf("discovery disabled: %v", err)
		}
	}()

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Home Streamer server %s (%q) listening on port %d", version, cfg.Name, cfg.Port)
	for _, ip := range lanIPs() {
		if isTailscale(ip) {
			log.Printf("  Tailscale (away from home) address: %s:%d  (in a browser: http://%s:%d/)", ip, cfg.Port, ip, cfg.Port)
		} else {
			log.Printf("  Home network address: http://%s:%d/  (open in a browser to test)", ip, cfg.Port)
		}
	}
	handler := logRequests(allowHosts(srv.routes()))
	// Also answer on the standard web port, so a browser works with just the
	// address (http://100.x.y.z) and no ":8095". Skipped if the port is taken.
	if cfg.Port != 80 {
		go func() {
			ln, err := net.Listen("tcp", ":80")
			if err != nil {
				return
			}
			log.Printf("  Also answering on port 80, so browsers work without \":%d\".", cfg.Port)
			(&http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}).Serve(ln)
		}()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal("Port %d is already in use, maybe by another copy of Home Streamer. (%v)", cfg.Port, err)
	}
	hs := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	runApp(cfg.Name, cfg.Port, *configPath, func() {
		fatal("%v", hs.Serve(ln))
	})
}

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func loadConfig(path string) (*Config, error) {
	host, _ := os.Hostname()
	cfg := &Config{Name: host, Port: 8095}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// First run: share the user's Videos folder and write a config to edit.
		if home, herr := os.UserHomeDir(); herr == nil {
			cfg.Folders = []string{filepath.Join(home, "Videos")}
		}
		if out, merr := json.MarshalIndent(cfg, "", "  "); merr == nil {
			if werr := os.WriteFile(path, out, 0o644); werr == nil {
				log.Printf("Created %s. Edit \"folders\" to choose what to share.", path)
			}
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	// Tolerate a UTF-8 BOM, which Notepad may add.
	data = []byte(strings.TrimPrefix(string(data), "\uFEFF"))
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w (tip: in JSON, write Windows paths with double backslashes, like \"D:\\\\Movies\")", path, err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8095
	}
	if cfg.Name == "" {
		cfg.Name = host
	}
	return cfg, nil
}

func lanIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
			ips = append(ips, n.IP.String())
		}
	}
	return ips
}

// isTailscale reports whether ip is in Tailscale's 100.64.0.0/10 range.
func isTailscale(ip string) bool {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	p := net.ParseIP(ip)
	return p != nil && cgnat.Contains(p)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/stream/") || r.Header.Get("Range") == "" || strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") {
			log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}
