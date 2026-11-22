package main

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// allowHosts refuses requests whose Host header is a public domain name.
// This blocks "DNS rebinding": a web page on some site pointing its own
// domain at this PC so the visitor's browser talks to Home Streamer. The
// apps and browsers reach the server by IP address, by the PC's name, or by
// a home/Tailscale name, which are all still allowed.
func allowHosts(h http.Handler) http.Handler {
	host, _ := os.Hostname()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func hostAllowed(hostport, pcName string) bool {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
	if h == "" {
		return true // HTTP/1.0 without Host: no browser sends this
	}
	if net.ParseIP(h) != nil || !strings.Contains(h, ".") {
		return true // an IP address, "localhost" or a bare PC name
	}
	if pc := strings.ToLower(pcName); pc != "" && h == pc {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".ts.net", ".beta.tailscale.net"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}
