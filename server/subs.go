package main

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var subExts = map[string]bool{".srt": true}

// Language tags commonly found in subtitle file names, mapped to ISO 639-1.
var langTags = map[string]string{
	"en": "en", "eng": "en", "english": "en",
	"he": "he", "heb": "he", "iw": "he", "hebrew": "he", "hb": "he",
	"ar": "ar", "ara": "ar", "arabic": "ar",
	"ru": "ru", "rus": "ru", "russian": "ru",
	"fr": "fr", "fre": "fr", "fra": "fr", "french": "fr",
	"es": "es", "spa": "es", "spanish": "es",
	"de": "de", "ger": "de", "deu": "de", "german": "de",
	"it": "it", "ita": "it", "italian": "it",
	"pt": "pt", "por": "pt", "portuguese": "pt",
	"nl": "nl", "dut": "nl", "nld": "nl", "dutch": "nl",
	"ja": "ja", "jpn": "ja", "japanese": "ja",
	"zh": "zh", "chi": "zh", "zho": "zh", "chinese": "zh",
	"ko": "ko", "kor": "ko", "korean": "ko",
	"tr": "tr", "tur": "tr", "turkish": "tr",
	"pl": "pl", "pol": "pl", "polish": "pl",
	"ro": "ro", "rum": "ro", "ron": "ro", "romanian": "ro",
	"hu": "hu", "hun": "hu", "hungarian": "hu",
	"el": "el", "gre": "el", "ell": "el", "greek": "el",
	"uk": "uk", "ukr": "uk", "ukrainian": "uk",
	"am": "am", "amh": "am", "amharic": "am",
}

var langNames = map[string]string{
	"en": "English", "he": "Hebrew", "ar": "Arabic", "ru": "Russian", "fr": "French",
	"es": "Spanish", "de": "German", "it": "Italian", "pt": "Portuguese", "nl": "Dutch",
	"ja": "Japanese", "zh": "Chinese", "ko": "Korean", "tr": "Turkish", "pl": "Polish",
	"ro": "Romanian", "hu": "Hungarian", "el": "Greek", "uk": "Ukrainian", "am": "Amharic",
}

// findSidecars returns subtitle files that belong to the video at abs.
// Files named "<video>.srt" or "<video>.<tags>.srt" in the same folder or a
// Subs/Subtitles subfolder match. If the video is alone in its folder, every
// .srt there matches too, because release folders often name subtitles differently.
func (l *Library) findSidecars(root int, rel, abs string, onlyVideo bool) []Subtitle {
	dir := filepath.Dir(abs)
	relDir := path.Dir(rel)
	if relDir == "." {
		relDir = ""
	}
	stem := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))

	var subs []Subtitle
	scan := func(d, rd string, inSubsFolder bool) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !subExts[strings.ToLower(filepath.Ext(name))] {
				continue
			}
			sstem := strings.TrimSuffix(name, filepath.Ext(name))
			var tags string
			switch {
			case strings.EqualFold(sstem, stem):
			case len(sstem) > len(stem) && strings.EqualFold(sstem[:len(stem)], stem) && strings.ContainsRune("._- ", rune(sstem[len(stem)])):
				tags = sstem[len(stem)+1:]
			case onlyVideo || inSubsFolder:
				tags = sstem
			default:
				continue
			}
			srel := name
			if rd != "" {
				srel = rd + "/" + name
			}
			id := makeID(root, srel)
			sub := Subtitle{ID: id, URL: "/subs/" + id}
			sub.Language, sub.Forced, sub.Label = parseTags(tags)
			if sub.Language == "" {
				sub.Language = sniffLanguage(filepath.Join(d, name))
			}
			sub.Label = buildLabel(sub, tags)
			subs = append(subs, sub)
		}
	}
	scan(dir, relDir, false)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() && isSubsFolder(e.Name()) {
				rd := e.Name()
				if relDir != "" {
					rd = relDir + "/" + e.Name()
				}
				scan(filepath.Join(dir, e.Name()), rd, onlyVideo)
				// Also support Subs/<video name>/*.srt (common for TV episodes).
				if inner := filepath.Join(dir, e.Name(), stem); isDir(inner) {
					scan(inner, rd+"/"+stem, true)
				}
			}
		}
	}
	return subs
}

var tagSplit = regexp.MustCompile(`[._\-\s\[\]()]+`)

func parseTags(tags string) (lang string, forced bool, extra string) {
	var rest []string
	for _, t := range tagSplit.Split(strings.ToLower(tags), -1) {
		if t == "" {
			continue
		}
		if l, ok := langTags[t]; ok && lang == "" {
			lang = l
			continue
		}
		if t == "forced" {
			forced = true
			continue
		}
		rest = append(rest, t)
	}
	return lang, forced, strings.Join(rest, " ")
}

func buildLabel(s Subtitle, tags string) string {
	_, _, extra := parseTags(tags)
	label := langNames[s.Language]
	if label == "" {
		label = "Subtitles"
	}
	if s.Forced {
		label += " (forced)"
	}
	if strings.Contains(extra, "sdh") || strings.Contains(extra, "cc") {
		label += " (SDH)"
	}
	return label + " · SRT file"
}

// sniffLanguage guesses a language from the script used in the file.
func sniffLanguage(p string) string {
	data, err := readHead(p, 16<<10)
	if err != nil {
		return ""
	}
	text := decodeTextPartial(data, true)
	var hebrew, arabic, cyrillic, latin int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Hebrew, r):
			hebrew++
		case unicode.Is(unicode.Arabic, r):
			arabic++
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		case unicode.IsLetter(r) && r < 0x250:
			latin++
		}
	}
	switch {
	case hebrew > 20 && hebrew*2 > latin:
		return "he"
	case arabic > 20 && arabic*2 > latin:
		return "ar"
	case cyrillic > 20 && cyrillic*2 > latin:
		return "ru"
	}
	return ""
}

func readHead(p string, n int) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	m, _ := f.Read(buf)
	return buf[:m], nil
}

// decodeText converts subtitle bytes to UTF-8. It handles BOMs, UTF-16, and
// the legacy Windows code pages older Hebrew and European subtitles use.
func decodeText(data []byte) string { return decodeTextPartial(data, false) }

// decodeTextPartial is decodeText for a buffer that may end mid-character.
func decodeTextPartial(data []byte, partial bool) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return string(data[3:])
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], true)
	}
	if utf8.Valid(data) || (partial && validUTF8Prefix(data)) {
		return string(data)
	}
	// Windows-1255 Hebrew letters are 0xE0-0xFA and appear in runs (whole
	// words), while Western accented letters such as é are usually isolated.
	var high, hebrewRun int
	for i, b := range data {
		if b < 0x80 {
			continue
		}
		high++
		if b >= 0xE0 && b <= 0xFA && ((i > 0 && data[i-1] >= 0xE0 && data[i-1] <= 0xFA) || (i+1 < len(data) && data[i+1] >= 0xE0 && data[i+1] <= 0xFA)) {
			hebrewRun++
		}
	}
	if high > 0 && hebrewRun*2 >= high {
		if s, err := charmap.Windows1255.NewDecoder().Bytes(data); err == nil {
			return string(s)
		}
	}
	s, _ := charmap.Windows1252.NewDecoder().Bytes(data)
	return string(s)
}

// validUTF8Prefix allows a multi-byte character cut off at the end of a buffer.
func validUTF8Prefix(data []byte) bool {
	for i := 1; i <= 3 && i <= len(data); i++ {
		if utf8.Valid(data[:len(data)-i]) && !utf8.Valid(data) {
			return !utf8.FullRune(data[len(data)-i:])
		}
	}
	return false
}

func decodeUTF16(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(u))
}

var srtTiming = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2}),(\d{1,3})`)

// srtToVTT converts SRT text to WebVTT for the browser test page.
func srtToVTT(srt string) string {
	srt = strings.ReplaceAll(srt, "\r\n", "\n")
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, line := range strings.Split(srt, "\n") {
		if strings.Contains(line, "-->") {
			line = srtTiming.ReplaceAllString(line, "$1.$2")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
