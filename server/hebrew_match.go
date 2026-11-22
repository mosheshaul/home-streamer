package main

// Finding Israeli titles on TMDB from names written in English letters, like
// "Haverot" (חברות) or "Lo Kara Clum" (לא קרה כלום).
//
// 1. Search for the name as written, in English and then in Hebrew, with and
//    without the year. TMDB knows many Israeli titles by their English
//    spelling, and every result carries its original (Hebrew) name.
// 2. If that finds nothing, spell the name in Hebrew letters a few likely
//    ways and search for those, accepting only a Hebrew original title that
//    has the same letters.

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// lookupVersion changes when matching improves, so names that weren't found
// before are looked up again.
const lookupVersion = 2

func (t *Titles) search(key, path, query, lang string, year int) ([]tmdbResult, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if lang != "" {
		q.Set("language", lang)
	}
	if year > 0 {
		if path == "/search/tv" {
			q.Set("first_air_date_year", fmt.Sprint(year))
		} else if path == "/search/movie" {
			q.Set("year", fmt.Sprint(year))
		}
	}
	var res struct {
		Results []tmdbResult `json:"results"`
	}
	if err := t.get(key, path, q, &res); err != nil {
		return nil, err
	}
	out := res.Results[:0]
	for _, r := range res.Results {
		if path == "/search/tv" {
			r.MediaType = "tv"
		} else if path == "/search/movie" {
			r.MediaType = "movie"
		}
		if r.MediaType == "movie" || r.MediaType == "tv" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (r *tmdbResult) year() string { return r.ReleaseDate + r.FirstAirDate }

func (r *tmdbResult) describe() string {
	return fmt.Sprintf("%s %q (%s)", r.MediaType, r.OriginalTitle+r.OriginalName, r.OriginalLang)
}

// sameName compares names ignoring case, spaces and punctuation.
func sameName(a, b string) bool {
	na, nb := normName(a), normName(b)
	return na != "" && na == nb
}

func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(finalToRegular(r))
		}
	}
	return b.String()
}

func finalToRegular(r rune) rune {
	switch r {
	case 'ך':
		return 'כ'
	case 'ם':
		return 'מ'
	case 'ן':
		return 'נ'
	case 'ף':
		return 'פ'
	case 'ץ':
		return 'צ'
	}
	return r
}

// pick chooses the result that matches the name: an Israeli title known by
// exactly this spelling first, then one from the right year, then TMDB's top
// result. confident is true when the result's name is the name searched for.
func pick(results []tmdbResult, query string, year int) (best *tmdbResult, confident bool) {
	yearOK := func(r *tmdbResult) bool { return year == 0 || strings.HasPrefix(r.year(), fmt.Sprint(year)) }
	named := func(r *tmdbResult) bool {
		return sameName(r.Title, query) || sameName(r.Name, query) ||
			sameName(r.OriginalTitle, query) || sameName(r.OriginalName, query)
	}
	for _, pass := range []func(r *tmdbResult) bool{
		func(r *tmdbResult) bool { return r.OriginalLang == "he" && named(r) && yearOK(r) },
		func(r *tmdbResult) bool { return r.OriginalLang == "he" && named(r) },
		func(r *tmdbResult) bool { return named(r) && yearOK(r) },
		func(r *tmdbResult) bool { return year > 0 && yearOK(r) },
	} {
		for i := range results {
			if pass(&results[i]) {
				return &results[i], named(&results[i])
			}
		}
	}
	if len(results) > 0 {
		return &results[0], false
	}
	return nil, false
}

// hebrewName returns the Hebrew name of an Israeli result, asking TMDB for
// its Hebrew details when the search result doesn't include it.
func (t *Titles) hebrewName(key string, r *tmdbResult) string {
	for _, n := range []string{r.OriginalTitle + r.OriginalName, r.Title + r.Name} {
		if hasHebrew(n) {
			return strings.TrimSpace(n)
		}
	}
	if r.ID == 0 {
		return ""
	}
	var d struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if t.get(key, fmt.Sprintf("/%s/%d", r.MediaType, r.ID), url.Values{"language": {"he-IL"}}, &d) == nil && hasHebrew(d.Title+d.Name) {
		return strings.TrimSpace(d.Title + d.Name)
	}
	return ""
}

// lookup searches TMDB and returns the Hebrew title when the match is Israeli.
func (t *Titles) lookup(key string, pn ParsedName) (hebrew, found string, err error) {
	path := "/search/multi"
	if pn.HasSeason {
		path = "/search/tv"
	}
	years := []int{0}
	if pn.Year > 0 && path != "/search/multi" {
		years = []int{pn.Year, 0}
	}
	for _, lang := range []string{"en-US", "he-IL"} {
		for _, y := range years {
			res, err := t.search(key, path, pn.Title, lang, y)
			if err != nil {
				return "", found, err
			}
			best, confident := pick(res, pn.Title, pn.Year)
			if best == nil {
				continue
			}
			found = best.describe()
			if best.OriginalLang == "he" {
				if he := t.hebrewName(key, best); he != "" {
					return he, found, nil
				}
			} else if confident {
				return "", found, nil // a foreign title by this very name
			}
		}
	}
	// Spell the name in Hebrew letters and search for that.
	for _, cand := range hebrewSpellings(pn.Title, 6) {
		res, err := t.search(key, path, cand, "he-IL", 0)
		if err != nil {
			return "", found, err
		}
		for i := range res {
			r := &res[i]
			orig := r.OriginalTitle + r.OriginalName
			if r.OriginalLang == "he" && sameSkeleton(orig, cand) {
				return strings.TrimSpace(orig), r.describe() + " via " + cand, nil
			}
		}
	}
	if found == "" {
		found = "nothing found"
	}
	return "", found, nil
}

// skeleton is a Hebrew name without the letters that are often left out or
// added in spelling (א ה ו י), and with similar-sounding letters merged.
func skeleton(s string) string {
	var b strings.Builder
	for _, r := range normName(s) {
		switch r {
		case 'א', 'ה', 'ו', 'י', 'ע', '\'', '׳':
			continue
		case 'ט':
			r = 'ת'
		case 'ק', 'ח':
			r = 'כ'
		case 'ש':
			r = 'ס'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sameSkeleton(a, b string) bool {
	sa := skeleton(a)
	return len([]rune(sa)) >= 2 && sa == skeleton(b)
}

// Common short words, spelled the way Hebrew titles write them.
var hebrewWords = map[string][]string{
	"lo": {"לא"}, "ha": {"ה"}, "ve": {"ו"}, "shel": {"של"}, "al": {"על"}, "im": {"עם"},
	"et": {"את"}, "ze": {"זה"}, "zot": {"זאת"}, "ani": {"אני"}, "ata": {"אתה"}, "at": {"את"},
	"hu": {"הוא"}, "hi": {"היא"}, "mi": {"מי"}, "ma": {"מה"}, "ken": {"כן"}, "yom": {"יום"},
	"ein": {"אין"}, "en": {"אין"}, "kol": {"כל"}, "od": {"עוד"}, "ad": {"עד"}, "eretz": {"ארץ"},
	"bait": {"בית"}, "beit": {"בית"}, "ben": {"בן"}, "bat": {"בת"}, "sof": {"סוף"}, "rosh": {"ראש"},
	"layla": {"לילה"}, "laila": {"לילה"}, "lev": {"לב"}, "tov": {"טוב"}, "tova": {"טובה"}, "ra": {"רע"},
	"ish": {"איש"}, "isha": {"אישה", "אשה"}, "yeled": {"ילד"}, "yalda": {"ילדה"}, "aba": {"אבא"}, "ima": {"אמא"},
	"sheli": {"שלי"}, "shelo": {"שלו"}, "shela": {"שלה"}, "kmo": {"כמו"}, "eich": {"איך"}, "lama": {"למה"},
	"po": {"פה"}, "sham": {"שם"}, "kan": {"כאן"}, "halev": {"הלב"}, "olam": {"עולם"}, "haolam": {"העולם"},
}

type spelling struct {
	s    string
	cost int
}

// Latin letter groups and their Hebrew spellings, most likely first. Longer
// groups are tried first.
var latinToHebrew = []struct {
	latin string
	heb   []string
}{
	{"tsh", []string{"צ'"}}, {"sch", []string{"ש"}},
	{"sh", []string{"ש"}}, {"ch", []string{"ח", "כ"}}, {"kh", []string{"ח", "כ"}},
	{"tz", []string{"צ"}}, {"ts", []string{"צ"}}, {"th", []string{"ת"}}, {"ph", []string{"פ"}},
	{"zh", []string{"ז'"}}, {"dj", []string{"ג'"}}, {"oo", []string{"ו"}}, {"ee", []string{"י"}},
	{"au", []string{"או"}}, {"ei", []string{"י"}}, {"ey", []string{"י"}}, {"ai", []string{"י", "אי"}}, {"ay", []string{"י"}},
	{"b", []string{"ב"}}, {"v", []string{"ב", "ו"}}, {"w", []string{"ו"}}, {"g", []string{"ג"}},
	{"d", []string{"ד"}}, {"h", []string{"ח", "ה"}}, {"z", []string{"ז"}}, {"t", []string{"ת", "ט"}},
	{"k", []string{"ק", "כ"}}, {"c", []string{"כ", "ק"}}, {"q", []string{"ק"}}, {"l", []string{"ל"}},
	{"m", []string{"מ"}}, {"n", []string{"נ"}}, {"s", []string{"ס", "ש"}}, {"p", []string{"פ"}},
	{"f", []string{"פ"}}, {"r", []string{"ר"}}, {"j", []string{"ג'", "י"}}, {"x", []string{"קס"}},
	{"y", []string{"י"}},
	{"a", []string{""}}, {"e", []string{"", "י"}}, {"i", []string{"י", ""}}, {"o", []string{"ו", ""}}, {"u", []string{"ו"}},
}

func spellWord(w string) []spelling {
	if d, ok := hebrewWords[w]; ok {
		out := make([]spelling, len(d))
		for i, s := range d {
			out[i] = spelling{s, i}
		}
		return out
	}
	partial := []spelling{{"", 0}}
	i := 0
	for i < len(w) {
		var opts []string
		n := 1
		for _, m := range latinToHebrew {
			if strings.HasPrefix(w[i:], m.latin) {
				opts, n = m.heb, len(m.latin)
				break
			}
		}
		if opts == nil { // not a letter we know: skip it
			i++
			continue
		}
		first, last := i == 0, i+n == len(w)
		if strings.ContainsRune("aeiou", rune(w[i])) {
			switch {
			case first && w[i] == 'o', first && w[i] == 'u':
				opts = []string{"או", "עו"}
			case first && (w[i] == 'i' || strings.HasPrefix(w[i:], "ei")):
				opts = []string{"אי", "עי"}
			case first:
				opts = []string{"א", "ע"}
			case last && w[i] == 'a', last && w[i] == 'e':
				opts = []string{"ה", "א", ""}
			case last && w[i] == 'o':
				opts = []string{"ו", "ה"}
			}
		} else if last && w[i] == 'h' {
			opts = []string{"ה", "ח"}
		}
		// A doubled consonant is written once.
		if i > 0 && w[i] == w[i-1] && !strings.ContainsRune("aeiou", rune(w[i])) {
			i += n
			continue
		}
		var next []spelling
		for _, p := range partial {
			for c, o := range opts {
				next = append(next, spelling{p.s + o, p.cost + c})
			}
		}
		sort.SliceStable(next, func(a, b int) bool { return next[a].cost < next[b].cost })
		if len(next) > 16 {
			next = next[:16]
		}
		partial = next
		i += n
	}
	for k := range partial {
		partial[k].s = withFinalLetters(partial[k].s)
	}
	return partial
}

func withFinalLetters(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	switch r[len(r)-1] {
	case 'כ':
		r[len(r)-1] = 'ך'
	case 'מ':
		r[len(r)-1] = 'ם'
	case 'נ':
		r[len(r)-1] = 'ן'
	case 'פ':
		r[len(r)-1] = 'ף'
	case 'צ':
		r[len(r)-1] = 'ץ'
	}
	return string(r)
}

// hebrewSpellings returns up to n likely Hebrew spellings of a name written
// in English letters, most likely first.
func hebrewSpellings(title string, n int) []string {
	words := strings.Fields(strings.ToLower(title))
	if len(words) == 0 || len(words) > 6 {
		return nil
	}
	all := []spelling{{"", 0}}
	for _, w := range words {
		ws := spellWord(w)
		if len(ws) == 0 {
			continue
		}
		var next []spelling
		for _, a := range all {
			for _, b := range ws {
				s := b.s
				if a.s != "" {
					s = a.s + " " + b.s
				}
				next = append(next, spelling{s, a.cost + b.cost})
			}
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].cost < next[j].cost })
		if len(next) > 32 {
			next = next[:32]
		}
		all = next
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range all {
		if s.s != "" && !seen[s.s] && len(out) < n {
			seen[s.s] = true
			out = append(out, s.s)
		}
	}
	return out
}
