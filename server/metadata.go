package main

// Hebrew titles for Israeli films and shows. Release names usually spell them
// in English letters ("Lo Kara Clum"), so the PC looks each title up on TMDB
// (themoviedb.org) and, when the original language is Hebrew, shows the
// original Hebrew name ("לא קרה כלום"). Results are cached in titles.json.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
)

type titleEntry struct {
	Hebrew  string    `json:"hebrew,omitempty"`
	Found   string    `json:"found,omitempty"` // what TMDB matched, for troubleshooting
	Checked time.Time `json:"checked"`
}

type Titles struct {
	mu      sync.Mutex
	key     string
	path    string
	m       map[string]*titleEntry
	queued  map[string]bool
	queue   chan ParsedName
	client  *http.Client
	baseURL string
}

func NewTitles(cachePath, apiKey string) *Titles {
	t := &Titles{key: strings.TrimSpace(apiKey), path: cachePath, m: map[string]*titleEntry{},
		queued: map[string]bool{}, queue: make(chan ParsedName, 10000),
		client: &http.Client{Timeout: 15 * time.Second}, baseURL: "https://api.themoviedb.org/3"}
	if data, err := os.ReadFile(cachePath); err == nil {
		json.Unmarshal(data, &t.m)
	}
	go t.worker()
	return t
}

func (t *Titles) Enabled() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.key != ""
}

func (t *Titles) SetKey(k string) {
	t.mu.Lock()
	t.key = strings.TrimSpace(k)
	// Retry titles that weren't found while there was no key.
	t.queued = map[string]bool{}
	t.mu.Unlock()
}

func titleKey(pn ParsedName) string {
	return strings.ToLower(pn.Title) + "|" + fmt.Sprint(pn.Year)
}

func hasHebrew(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hebrew, r) {
			return true
		}
	}
	return false
}

func worthLookingUp(pn ParsedName) bool {
	if len([]rune(pn.Title)) < 3 || hasHebrew(pn.Title) || isAllDigits(strings.ReplaceAll(pn.Title, " ", "")) {
		return false
	}
	switch strings.ToLower(pn.Title) {
	case "movies", "films", "tv", "tv shows", "series", "shows", "videos", "kids", "downloads", "subs", "subtitles", "sample", "extras", "featurettes":
		return false
	}
	return true
}

// Hebrew returns the Hebrew title for pn if known, and queues a lookup if not.
func (t *Titles) Hebrew(pn ParsedName) (string, bool) {
	if t == nil || !worthLookingUp(pn) {
		return "", false
	}
	k := titleKey(pn)
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.m[k]; ok && (e.Hebrew != "" || time.Since(e.Checked) < 30*24*time.Hour) {
		return e.Hebrew, e.Hebrew != ""
	}
	if t.key != "" && !t.queued[k] {
		t.queued[k] = true
		select {
		case t.queue <- pn:
		default:
		}
	}
	return "", false
}

func (t *Titles) worker() {
	for pn := range t.queue {
		t.mu.Lock()
		key := t.key
		t.mu.Unlock()
		if key == "" {
			continue
		}
		he, found, err := t.lookup(key, pn)
		if err != nil {
			log.Printf("Title lookup for %q failed: %v", pn.Title, err)
			t.mu.Lock()
			delete(t.queued, titleKey(pn)) // try again later
			t.mu.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}
		if he != "" {
			log.Printf("Hebrew title: %s → %s", pn.Title, he)
		}
		t.mu.Lock()
		t.m[titleKey(pn)] = &titleEntry{Hebrew: he, Found: found, Checked: time.Now()}
		data, _ := json.MarshalIndent(t.m, "", "  ")
		t.mu.Unlock()
		tmp := t.path + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			os.Rename(tmp, t.path)
		}
		time.Sleep(250 * time.Millisecond) // stay well under TMDB's rate limit
	}
}

type tmdbResult struct {
	MediaType     string `json:"media_type"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	OriginalLang  string `json:"original_language"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
}

func (t *Titles) get(key, path string, q url.Values, out any) error {
	if len(key) > 60 { // a "Read Access Token" rather than an API key
		// sent as a header below
	} else {
		q.Set("api_key", key)
	}
	req, _ := http.NewRequest("GET", t.baseURL+path+"?"+q.Encode(), nil)
	if len(key) > 60 {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		// Don't repeat the URL: it contains the key.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("couldn't reach TMDB (%v)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("TMDB rejected the key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// lookup searches TMDB and returns the Hebrew title when the match is Israeli.
func (t *Titles) lookup(key string, pn ParsedName) (hebrew, found string, err error) {
	q := url.Values{"query": {pn.Title}, "language": {"he-IL"}, "include_adult": {"false"}}
	path := "/search/multi"
	if pn.HasSeason {
		path = "/search/tv"
		if pn.Year > 0 {
			q.Set("first_air_date_year", fmt.Sprint(pn.Year))
		}
	}
	var res struct {
		Results []tmdbResult `json:"results"`
	}
	if err := t.get(key, path, q, &res); err != nil {
		return "", "", err
	}
	var best *tmdbResult
	for i := range res.Results {
		r := &res.Results[i]
		if pn.HasSeason {
			r.MediaType = "tv"
		}
		if r.MediaType != "movie" && r.MediaType != "tv" {
			continue
		}
		year := r.ReleaseDate + r.FirstAirDate
		if pn.Year > 0 && strings.HasPrefix(year, fmt.Sprint(pn.Year)) {
			best = r
			break
		}
		if best == nil {
			best = r
		}
	}
	if best == nil {
		return "", "", nil
	}
	orig := best.OriginalTitle + best.OriginalName
	local := best.Title + best.Name
	found = fmt.Sprintf("%s %q (%s)", best.MediaType, orig, best.OriginalLang)
	if best.OriginalLang != "he" {
		return "", found, nil
	}
	if hasHebrew(orig) {
		return orig, found, nil
	}
	if hasHebrew(local) {
		return local, found, nil
	}
	return "", found, nil
}

// CheckKey makes one request to confirm a key works.
func (t *Titles) CheckKey(key string) error {
	var out map[string]any
	return t.get(strings.TrimSpace(key), "/configuration", url.Values{}, &out)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"hebrewTitles": s.lib.Titles.Enabled()})
}

// handleSetTMDB checks a TMDB key and saves it in config.json. The key is
// never sent back to the apps.
func (s *Server) handleSetTMDB(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key := strings.TrimSpace(body.Key)
	if s.lib.Titles == nil {
		http.Error(w, "Hebrew titles aren't available", http.StatusInternalServerError)
		return
	}
	if key != "" {
		if err := s.lib.Titles.CheckKey(key); err != nil {
			http.Error(w, "That key didn't work: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := s.saveConfigField("tmdbKey", key); err != nil {
		http.Error(w, "Couldn't save config.json: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.lib.Titles.SetKey(key)
	if key != "" {
		log.Printf("TMDB key saved: Hebrew titles are on.")
	}
	w.WriteHeader(http.StatusNoContent)
}

// saveConfigField sets one field in config.json, keeping everything else as written.
func (s *Server) saveConfigField(name, value string) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	m := map[string]any{}
	if data, err := os.ReadFile(s.cfgPath); err == nil {
		data = []byte(strings.TrimPrefix(string(data), "\uFEFF"))
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
	}
	if value == "" {
		delete(m, name)
	} else {
		m[name] = value
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.cfgPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.cfgPath)
}
