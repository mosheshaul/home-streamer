package main

// Friendlier names: release names like "Heart.of.the.Beast.2026.2160p.WEB-DL.DDP5.1-GTM"
// are shown as "Heart of the Beast", and Israeli titles in Hebrew (metadata.go).

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reSeasonEp = regexp.MustCompile(`(?i)^s(\d{1,2})[ ._-]?e(\d{1,3})(?:[ ._-]?e?(\d{1,3}))?$`)
	reSeason   = regexp.MustCompile(`(?i)^s(\d{1,2})$`)
	reEpOnly   = regexp.MustCompile(`(?i)^(?:e|ep)(\d{1,3})$`)
	reXEp      = regexp.MustCompile(`(?i)^(\d{1,2})x(\d{2,3})$`)
	reYear     = regexp.MustCompile(`^\(?(19[2-9]\d|20[0-4]\d)\)?$`)
	reJunk     = regexp.MustCompile(`(?i)^(\d{3,4}p|4k|uhd|hd|fhd|sd|web|web-?dl|webrip|web-?rip|bluray|blu-ray|bdrip|brrip|bdremux|remux|dvdrip|dvd|dvdscr|hdtv|hdrip|hdcam|cam|ts|tc|r5|repack|proper|real|rerip|extended|unrated|uncut|remastered|imax|dc|limited|internal|multi|dual|subs?|hebsub|heb|hebrew|eng|x264|x265|h264|h265|h\.264|h\.265|hevc|avc|xvid|divx|10bit|8bit|hdr|hdr10|hdr10\+|dv|dovi|sdr|ddp?\d?(\.\d)?|dd\+?\d?(\.\d)?|eac3|ac3|aac\d?(\.\d)?|dts(-?hd)?|truehd|atmos|flac|mp3|\d\.\d|5\.1|7\.1|2\.0|nf|amzn|dsnp|hmax|atvp|it|hulu|pcok|yts|yify|rarbg|complete|season|series)$`)
)

// ParsedName is a release name split into its useful parts.
type ParsedName struct {
	Title            string
	Year             int
	Season, Episode  int // 0 when absent
	Episode2         int // for double episodes like S01E01-E02
	HasSeason, HasEp bool
}

// parseName reads a release-style file or folder name.
func parseName(raw string) ParsedName {
	var pn ParsedName
	s := reBrackets.ReplaceAllString(raw, " ")
	s = strings.NewReplacer("_", " ").Replace(s)
	words := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '.' })
	var title []string
	ended := false
	for i, w := range words {
		bare := strings.Trim(w, "-()")
		if bare == "" {
			continue
		}
		if m := reSeasonEp.FindStringSubmatch(bare); m != nil {
			pn.Season, pn.Episode, pn.HasSeason, pn.HasEp = atoi(m[1]), atoi(m[2]), true, true
			pn.Episode2 = atoi(m[3])
			break
		}
		if m := reXEp.FindStringSubmatch(bare); m != nil {
			pn.Season, pn.Episode, pn.HasSeason, pn.HasEp = atoi(m[1]), atoi(m[2]), true, true
			break
		}
		if m := reEpOnly.FindStringSubmatch(bare); m != nil && len(title) > 0 {
			pn.Episode, pn.HasEp = atoi(m[1]), true
			break
		}
		if len(title) > 0 && i+1 < len(words) && (strings.EqualFold(bare, "episode") || strings.EqualFold(bare, "ep") || bare == "פרק") {
			if n := strings.Trim(words[i+1], "-()"); isAllDigits(n) {
				pn.Episode, pn.HasEp = atoi(n), true
				break
			}
		}
		if m := reSeason.FindStringSubmatch(bare); m != nil {
			pn.Season, pn.HasSeason = atoi(m[1]), true
			break
		}
		if strings.EqualFold(bare, "season") && i+1 < len(words) {
			if n := strings.Trim(words[i+1], "-()"); isAllDigits(n) {
				pn.Season, pn.HasSeason = atoi(n), true
				break
			}
		}
		// A year or a technical tag ends the title (but never as the first word,
		// so films like "1917" or "2012" keep their name).
		if len(title) > 0 && reYear.MatchString(bare) {
			if pn.Year == 0 {
				pn.Year = atoi(strings.Trim(bare, "()"))
			}
			ended = true
			continue // a season tag may still follow the year
		}
		if len(title) > 0 && (reJunk.MatchString(bare) || reJunk.MatchString(strings.SplitN(bare, "-", 2)[0])) {
			ended = true
			continue
		}
		if !ended {
			title = append(title, w)
		}
	}
	t := strings.Trim(strings.Join(title, " "), " -–()")
	pn.Title = strings.Join(strings.Fields(t), " ")
	return pn
}

// suffix is the season/episode part, in English or Hebrew.
func (pn ParsedName) suffix(hebrew bool) string {
	switch {
	case pn.HasEp && !pn.HasSeason && hebrew:
		return fmt.Sprintf("פרק %d", pn.Episode)
	case pn.HasEp && !pn.HasSeason:
		return fmt.Sprintf("Episode %d", pn.Episode)
	case pn.HasEp && hebrew:
		s := fmt.Sprintf("עונה %d פרק %d", pn.Season, pn.Episode)
		if pn.Episode2 > 0 {
			s += fmt.Sprintf("-%d", pn.Episode2)
		}
		return s
	case pn.HasEp:
		s := "S" + pad2(fmt.Sprint(pn.Season)) + "E" + pad2(fmt.Sprint(pn.Episode))
		if pn.Episode2 > 0 {
			s += "-E" + pad2(fmt.Sprint(pn.Episode2))
		}
		return s
	case pn.HasSeason && hebrew:
		return fmt.Sprintf("עונה %d", pn.Season)
	case pn.HasSeason:
		return fmt.Sprintf("Season %d", pn.Season)
	}
	return ""
}

// compose joins a title with the season/episode part.
func (pn ParsedName) compose(title string, hebrew bool) string {
	suf := pn.suffix(hebrew)
	switch {
	case title == "":
		return suf
	case suf == "":
		return title
	case hebrew:
		return title + " – " + suf
	}
	return title + " " + suf
}

// cleanName turns a release-style file or folder name into a readable title.
func cleanName(raw string) string {
	if t := parseName(raw).compose(parseName(raw).Title, false); t != "" {
		return t
	}
	return raw
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
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

// display returns the name to show for an item: the Hebrew title for Israeli
// films and shows (when a TMDB key is set), else the cleaned-up name.
func (l *Library) display(raw string) string {
	pn := parseName(raw)
	if he, ok := l.Titles.Hebrew(pn); ok {
		return pn.compose(he, true)
	}
	return cleanName(raw)
}
