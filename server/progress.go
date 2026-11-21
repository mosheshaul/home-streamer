package main

// Watch progress for the browser player, stored on the PC so it's the same
// whichever address (home or Tailscale) the laptop uses.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type Progress struct {
	ID       string          `json:"id"`
	Position float64         `json:"position"` // seconds
	Duration float64         `json:"duration"`
	Updated  time.Time       `json:"updated"`
	Item     json.RawMessage `json:"item"` // the video's listing entry, for "Continue watching"
}

type ProgressStore struct {
	mu   sync.Mutex
	path string
	m    map[string]*Progress
}

func NewProgressStore(path string) *ProgressStore {
	ps := &ProgressStore{path: path, m: map[string]*Progress{}}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &ps.m)
	}
	return ps
}

func (ps *ProgressStore) save() {
	data, _ := json.MarshalIndent(ps.m, "", " ")
	tmp := ps.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, ps.path)
	}
}

func (ps *ProgressStore) handleList(w http.ResponseWriter, r *http.Request) {
	ps.mu.Lock()
	list := make([]*Progress, 0, len(ps.m))
	for _, p := range ps.m {
		list = append(list, p)
	}
	ps.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Updated.After(list[j].Updated) })
	if len(list) > 30 {
		list = list[:30]
	}
	writeJSON(w, list)
}

func (ps *ProgressStore) handlePut(w http.ResponseWriter, r *http.Request) {
	var p Progress
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&p); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p.ID = r.PathValue("id")
	p.Updated = time.Now()
	ps.mu.Lock()
	// Near the start or the end means "not in progress".
	if p.Position < 15 || (p.Duration > 0 && p.Position > p.Duration-60 && p.Position > p.Duration*0.93) {
		delete(ps.m, p.ID)
	} else {
		ps.m[p.ID] = &p
	}
	ps.save()
	ps.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (ps *ProgressStore) handleDelete(w http.ResponseWriter, r *http.Request) {
	ps.mu.Lock()
	delete(ps.m, r.PathValue("id"))
	ps.save()
	ps.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
