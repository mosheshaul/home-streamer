package main

// Friendlier names: release names like "Heart.of.the.Beast.2026.2160p.WEB-DL.DDP5.1-GTM"
// are shown as "Heart of the Beast", and any folder or video can be given a
// custom name (for example in Hebrew) that every device sees.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
)

var (
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reSeasonEp = regexp.MustCompile(`(?i)^s(\d{1,2})[ ._-]?e(\d{1,3})(?:[ ._-]?e?(\d{1,3}))?$`)
	reSeason   = regexp.MustCompile(`(?i)^s(\d{1,2})$`)
	reXEp      = regexp.MustCompile(`(?i)^(\d{1,2})x(\d{2,3})$`)
	reYear     = regexp.MustCompile(`^\(?(19[2-9]\d|20[0-4]\d)\)?$`)
	reJunk     = regexp.MustCompile(`(?i)^(\d{3,4}p|4k|uhd|hd|fhd|sd|web|web-?dl|webrip|web-?rip|bluray|blu-ray|bdrip|brrip|bdremux|remux|dvdrip|dvd|dvdscr|hdtv|hdrip|hdcam|cam|ts|tc|r5|repack|proper|real|rerip|extended|unrated|uncut|remastered|imax|dc|limited|internal|multi|dual|subs?|hebsub|heb|hebrew|eng|x264|x265|h264|h265|h\.264|h\.265|hevc|avc|xvid|divx|10bit|8bit|hdr|hdr10|hdr10\+|dv|dovi|sdr|ddp?\d?(\.\d)?|dd\+?\d?(\.\d)?|eac3|ac3|aac\d?(\.\d)?|dts(-?hd)?|truehd|atmos|flac|mp3|\d\.\d|5\.1|7\.1|2\.0|nf|amzn|dsnp|hmax|atvp|it|hulu|pcok|yts|yify|rarbg|complete|season|series)$`)
)

// cleanName turns a release-style file or folder name into a readable title.
func cleanName(raw string) string {
	s := reBrackets.ReplaceAllString(raw, " ")
	// Dots and underscores separate words in release names; keep "5.1"-style
	// numbers intact by splitting only on dots between letters/numbers groups.
	s = strings.NewReplacer("_", " ").Replace(s)
	words := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '.' })
	// Join tokens like "DDP5" "1" back would be pointless: they are junk anyway.

	var title []string
	var suffix string
	for i, w := range words {
		bare := strings.Trim(w, "-()")
		if bare == "" {
			continue
		}
		if m := reSeasonEp.FindStringSubmatch(bare); m != nil {
			suffix = "S" + pad2(m[1]) + "E" + pad2(m[2])
			if m[3] != "" {
				suffix += "-E" + pad2(m[3])
			}
			break
		}
		if m := reXEp.FindStringSubmatch(bare); m != nil {
			suffix = "S" + pad2(m[1]) + "E" + pad2(m[2])
			break
		}
		if m := reSeason.FindStringSubmatch(bare); m != nil {
			suffix = "Season " + strings.TrimLeft(m[1], "0")
			break
		}
		if strings.EqualFold(bare, "season") && i+1 < len(words) {
			if n := strings.Trim(words[i+1], "-()"); isAllDigits(n) {
				suffix = "Season " + strings.TrimLeft(n, "0")
				break
			}
		}
		// A year or a technical tag ends the title (but never as the first word,
		// so films like "1917" or "2012" keep their name).
		if len(title) > 0 && (reYear.MatchString(bare) || reJunk.MatchString(bare) || reJunk.MatchString(strings.SplitN(bare, "-", 2)[0])) {
			break
		}
		title = append(title, w)
	}
	t := strings.Join(title, " ")
	// "Name-GROUP" leftovers and stray separators.
	t = strings.Trim(t, " -–()")
	t = strings.Join(strings.Fields(t), " ")
	if suffix != "" {
		if t == "" {
			return suffix
		}
		t += " " + suffix
	}
	if t == "" {
		return raw
	}
	return t
}

func pad2(s string) string {
	s = strings.TrimLeft(s, "0")
	if len(s) < 2 {
		s = strings.Repeat("0", 2-len(s)) + s
	}
	return s
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// NameStore keeps custom names chosen in the apps, in names.json next to config.json.
type NameStore struct {
	mu   sync.Mutex
	path string
	m    map[string]string // item id -> name
}

func NewNameStore(path string) *NameStore {
	ns := &NameStore{path: path, m: map[string]string{}}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &ns.m)
	}
	return ns
}

func (ns *NameStore) Get(id string) (string, bool) {
	if ns == nil {
		return "", false
	}
	ns.mu.Lock()
	defer ns.mu.Unlock()
	n, ok := ns.m[id]
	return n, ok
}

func (ns *NameStore) set(id, name string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if name == "" {
		delete(ns.m, id)
	} else {
		ns.m[id] = name
	}
	data, _ := json.MarshalIndent(ns.m, "", "  ")
	tmp := ns.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, ns.path)
	}
}

// display returns the name to show for an item: custom, else cleaned up.
func (l *Library) display(id, raw string) string {
	if n, ok := l.Names.Get(id); ok {
		return n
	}
	return cleanName(raw)
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, _, err := s.lib.Resolve(id); err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if len([]rune(name)) > 200 {
		http.Error(w, "name too long", http.StatusBadRequest)
		return
	}
	s.lib.Names.set(id, name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResetName(w http.ResponseWriter, r *http.Request) {
	s.lib.Names.set(r.PathValue("id"), "")
	w.WriteHeader(http.StatusNoContent)
}
