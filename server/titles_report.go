package main

// A page that shows what the PC found for each title (http://<PC>:8095/titles),
// so it's easy to see why a name isn't shown in Hebrew.

import (
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (s *Server) handleTitlesReport(w http.ResponseWriter, r *http.Request) {
	t := s.lib.Titles
	type row struct {
		name string
		e    titleEntry
	}
	var rows []row
	var on bool
	var lastErr string
	var lastAt time.Time
	pending := 0
	if t != nil {
		t.mu.Lock()
		on = t.key != ""
		lastErr, lastAt = t.lastErr, t.lastAt
		for k, e := range t.m {
			rows = append(rows, row{strings.SplitN(k, "|", 2)[0], *e})
		}
		for k := range t.queued {
			if _, done := t.m[k]; !done {
				pending++
			}
		}
		t.mu.Unlock()
	}
	sort.Slice(rows, func(i, j int) bool {
		if (rows[i].e.Hebrew == "") != (rows[j].e.Hebrew == "") {
			return rows[i].e.Hebrew == "" // not found first
		}
		return rows[i].name < rows[j].name
	})
	found := 0
	for _, r := range rows {
		if r.e.Hebrew != "" {
			found++
		}
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Hebrew titles</title><style>
body{font:15px system-ui,sans-serif;background:#0f1318;color:#e8eaed;margin:0;padding:16px}
h1{font-size:22px} .muted{color:#9aa0a6} table{border-collapse:collapse;width:100%;max-width:960px}
td,th{text-align:left;padding:6px 8px;border-bottom:1px solid #232a33;vertical-align:top} .no{color:#f28b82} .yes{color:#81c995}
button{font:inherit;padding:6px 14px;border-radius:6px;border:0;background:#1a73e8;color:#fff;cursor:pointer} .err{color:#f28b82}
</style></head><body><h1>Hebrew titles</h1>`)
	fmt.Fprintf(&b, `<p>Version %s. `, html.EscapeString(version))
	if on {
		b.WriteString(`TMDB key: <span class="yes">saved</span>.`)
	} else {
		b.WriteString(`TMDB key: <span class="no">not set</span>. Add it in Settings → Hebrew titles.`)
	}
	fmt.Fprintf(&b, ` %d titles checked, %d in Hebrew.`, len(rows), found)
	if pending > 0 {
		fmt.Fprintf(&b, ` %d still being looked up (refresh in a minute).`, pending)
	}
	b.WriteString(`</p>`)
	if lastErr != "" {
		fmt.Fprintf(&b, `<p class="err">Last error (%s): %s</p>`, lastAt.Format("Jan 2 15:04"), html.EscapeString(lastErr))
	}
	b.WriteString(`<p><button onclick="fetch('/api/titles/retry',{method:'POST'}).then(()=>{this.textContent='Done. Open your folders in the app, then refresh this page.'})">Look up the missing ones again</button></p>`)
	b.WriteString(`<table><tr><th>Name from the file</th><th>Shown as</th><th>What TMDB matched</th></tr>`)
	for _, r := range rows {
		shown := `<span class="no">not changed</span>`
		if r.e.Hebrew != "" {
			shown = `<span class="yes" dir="rtl">` + html.EscapeString(r.e.Hebrew) + `</span>`
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td class="muted" dir="auto">%s</td></tr>`,
			html.EscapeString(r.name), shown, html.EscapeString(r.e.Found))
	}
	b.WriteString(`</table>`)
	if len(rows) == 0 {
		b.WriteString(`<p class="muted">Nothing checked yet. Titles are looked up when a folder is opened in the app.</p>`)
	}
	b.WriteString(`</body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(b.String()))
}

// handleTitlesRetry forgets the titles that weren't found, so they're looked up again.
func (s *Server) handleTitlesRetry(w http.ResponseWriter, r *http.Request) {
	if t := s.lib.Titles; t != nil {
		t.mu.Lock()
		for k, e := range t.m {
			if e.Hebrew == "" {
				delete(t.m, k)
			}
		}
		t.queued = map[string]bool{}
		t.lastErr = ""
		t.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}
