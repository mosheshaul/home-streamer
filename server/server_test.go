package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
)

func TestDecodeText(t *testing.T) {
	heb := "שלום לכולם, זה מבחן של כתוביות בעברית"
	cp, _ := charmap.Windows1255.NewEncoder().String(heb)
	cases := map[string][]byte{
		"utf8":    []byte(heb),
		"utf8bom": append([]byte{0xEF, 0xBB, 0xBF}, heb...),
		"cp1255":  []byte(cp),
		"utf16le": utf16le(heb),
	}
	for name, in := range cases {
		if got := decodeText(in); got != heb {
			t.Errorf("%s: got %q", name, got)
		}
	}
	if got := decodeText([]byte{'C', 'a', 'f', 0xE9}); got != "Café" {
		t.Errorf("cp1252: got %q", got)
	}
	fr := "Où est la bibliothèque? Très bien, merci. À bientôt, garçon."
	frb, _ := charmap.Windows1252.NewEncoder().String(fr)
	if got := decodeText([]byte(frb)); got != fr {
		t.Errorf("french cp1252: got %q", got)
	}
}

func utf16le(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

func TestParseTags(t *testing.T) {
	for in, want := range map[string]string{"en": "en", "heb": "he", "Hebrew.forced": "he", "eng.sdh": "en", "": ""} {
		if got, _, _ := parseTags(in); got != want {
			t.Errorf("parseTags(%q) = %q, want %q", in, got, want)
		}
	}
	if _, forced, _ := parseTags("he.forced"); !forced {
		t.Error("expected forced")
	}
}

func TestNaturalLess(t *testing.T) {
	if !naturalLess("Episode 2", "Episode 10") || naturalLess("Episode 10", "Episode 2") {
		t.Error("natural order broken")
	}
}

func TestResolveBlocksTraversal(t *testing.T) {
	dir := t.TempDir()
	lib := NewLibrary([]string{dir})
	for _, rel := range []string{"../secret", "a/../../secret", "/../../etc/passwd"} {
		_, _, abs, err := lib.Resolve(makeID(0, rel))
		if err == nil {
			if r, _ := filepath.Rel(dir, abs); r == ".." || len(r) > 2 && r[:3] == ".."+string(filepath.Separator) {
				t.Errorf("%q escaped to %s", rel, abs)
			}
		}
	}
	if _, _, _, err := lib.Resolve("!!!"); err == nil {
		t.Error("expected error for garbage id")
	}
}

func TestSidecars(t *testing.T) {
	dir := t.TempDir()
	touch := func(n string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, n)), 0o755)
		os.WriteFile(filepath.Join(dir, n), []byte("1\n"), 0o644)
	}
	touch("Ep 1.mkv")
	touch("Ep 2.mkv")
	touch("Ep 1.srt")
	touch("Ep 1.he.srt")
	touch("Ep 2.eng.forced.srt")
	touch("unrelated.srt")
	touch("Subs/Ep 2/English.srt")
	lib := NewLibrary([]string{dir})
	l, err := lib.Browse("")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 2 {
		t.Fatalf("items: %+v", l.Items)
	}
	if n := len(l.Items[0].Subtitles); n != 2 {
		t.Errorf("Ep 1 subs = %d, want 2: %+v", n, l.Items[0].Subtitles)
	}
	if n := len(l.Items[1].Subtitles); n != 2 {
		t.Errorf("Ep 2 subs = %d, want 2: %+v", n, l.Items[1].Subtitles)
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"Heart.of.the.Beast.2026.2160p.REPACK.iT.WEB-DL.DDP5.1.Atmos.DV.HDR10+.H.265-GTM": "Heart of the Beast",
		"Lo.Kara.Clum.S01.1080p.WEB-DL.AAC2.0.H.264":                                      "Lo Kara Clum Season 1",
		"Lo.Kara.Clum.S01E03.1080p.WEB-DL":                                                "Lo Kara Clum S01E03",
		"The.Escape.IL.S01.1080p":                                                         "The Escape Season 1",
		"Haverot.Ep05.HDTV.x264":                                                          "Haverot Episode 5",
		"Haverot - Episode 12 [HEB]":                                                      "Haverot Episode 12",
		"The Matrix (1999) [1080p] [YTS.MX]":                                              "The Matrix",
		"1917.2019.1080p.BluRay.x264":                                                     "1917",
		"Some_Show_-_Season_2":                                                            "Some Show Season 2",
		"Old Movie":                                                                       "Old Movie",
		"לא קרה כלום":                                                                     "לא קרה כלום",
		"Show.Name.2x05.HDTV":                                                             "Show Name S02E05",
	}
	for in, want := range cases {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHebrewTitles(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Query().Get("query") {
		case "Lo Kara Clum":
			if r.URL.Path != "/search/tv" {
				t.Errorf("show searched at %s", r.URL.Path)
			}
			w.Write([]byte(`{"results":[{"name":"לא קרה כלום","original_name":"לא קרה כלום","original_language":"he","first_air_date":"2023-01-01"}]}`))
		case "חברות":
			w.Write([]byte(`{"results":[{"name":"חברות","original_name":"חברות","original_language":"he"}]}`))
		case "The Matrix":
			w.Write([]byte(`{"results":[{"media_type":"movie","title":"מטריקס","original_title":"The Matrix","original_language":"en","release_date":"1999-03-31"}]}`))
		default:
			w.Write([]byte(`{"results":[]}`))
		}
	}))
	defer ts.Close()
	tt := NewTitles(filepath.Join(t.TempDir(), "titles.json"), "k")
	tt.baseURL = ts.URL
	lib := &Library{Titles: tt}
	for _, raw := range []string{"Lo.Kara.Clum.S01E03.1080p.WEB-DL", "The Matrix (1999) [1080p]", "Haverot.S01.1080p.WEB-DL"} {
		lib.display(raw) // queues the lookup
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		tt.mu.Lock()
		n := len(tt.m)
		tt.mu.Unlock()
		if n == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := lib.display("Lo.Kara.Clum.S01E03.1080p.WEB-DL"); got != "לא קרה כלום – עונה 1 פרק 3" {
		t.Errorf("Israeli show: got %q", got)
	}
	if got := lib.display("Haverot.S01.1080p.WEB-DL"); got != "חברות – עונה 1" {
		t.Errorf("show found by its Hebrew spelling: got %q", got)
	}
	if got := lib.display("The Matrix (1999) [1080p]"); got != "The Matrix" {
		t.Errorf("foreign film: got %q", got)
	}
	if err := tt.CheckKey("bad"); err == nil {
		t.Error("bad key accepted")
	}
}

func TestHebrewSpellings(t *testing.T) {
	for in, want := range map[string]string{"Haverot": "חברות", "Lo Kara Clum": "לא קרה כלום", "Srugim": "סרוגים", "Fauda": "פאודה", "Ramzor": "רמזור"} {
		got := hebrewSpellings(in, 6)
		ok := false
		for _, g := range got {
			ok = ok || g == want
		}
		if !ok {
			t.Errorf("%s: %q not in %v", in, want, got)
		}
	}
}
