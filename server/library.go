package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var videoExts = map[string]string{
	".mkv":  "video/x-matroska",
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".ts":   "video/mp2t",
	".m2ts": "video/mp2t",
	".wmv":  "video/x-ms-wmv",
	".mpg":  "video/mpeg",
	".mpeg": "video/mpeg",
	".flv":  "video/x-flv",
}

// Root is one shared folder.
type Root struct {
	Name string
	Path string
}

// Library maps opaque ids to files inside the shared folders.
// An id is base64url("<rootIndex>:<relative/slash/path>").
type Library struct {
	Roots []Root
	Names *NameStore
}

func NewLibrary(folders []string) *Library {
	lib := &Library{}
	seen := map[string]int{}
	for _, f := range folders {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			fmt.Fprintf(os.Stderr, "WARNING: skipping %q: not a folder\n", f)
			continue
		}
		name := filepath.Base(abs)
		if name == "" || name == "." || name == string(filepath.Separator) || strings.HasSuffix(name, ":\\") {
			name = abs
		}
		seen[name]++
		if seen[name] > 1 {
			name = fmt.Sprintf("%s (%d)", name, seen[name])
		}
		lib.Roots = append(lib.Roots, Root{Name: name, Path: abs})
	}
	return lib
}

func makeID(root int, rel string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(root) + ":" + rel))
}

var errBadID = errors.New("invalid id")

// Resolve turns an id into (root index, slash-relative path, absolute path).
func (l *Library) Resolve(id string) (int, string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return 0, "", "", errBadID
	}
	idx, rel, ok := strings.Cut(string(raw), ":")
	if !ok {
		return 0, "", "", errBadID
	}
	n, err := strconv.Atoi(idx)
	if err != nil || n < 0 || n >= len(l.Roots) {
		return 0, "", "", errBadID
	}
	rel = path.Clean("/" + rel)[1:] // strips any ../ segments
	root := l.Roots[n].Path
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if r, err := filepath.Rel(root, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return 0, "", "", errBadID
	}
	return n, rel, abs, nil
}

type Subtitle struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Language string `json:"language,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	URL      string `json:"url"`
}

type Item struct {
	Type      string     `json:"type"` // "folder" or "video"
	ID        string     `json:"id"`
	Name      string     `json:"name"`               // name to show (custom or cleaned up)
	FileName  string     `json:"fileName,omitempty"` // the real file or folder name
	Size      int64      `json:"size,omitempty"`
	Modified  time.Time  `json:"modified,omitzero"`
	MimeType  string     `json:"mimeType,omitempty"`
	StreamURL string     `json:"streamUrl,omitempty"`
	Subtitles []Subtitle `json:"subtitles,omitempty"`
}

type Listing struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parentId,omitempty"`
	Items    []Item `json:"items"`
}

// Browse lists a folder. The empty id lists the shared roots.
func (l *Library) Browse(id string) (*Listing, error) {
	if id == "" {
		out := &Listing{Title: "Library", Items: []Item{}}
		for i, r := range l.Roots {
			rid := makeID(i, "")
			name := r.Name
			if n, ok := l.Names.Get(rid); ok {
				name = n
			}
			out.Items = append(out.Items, Item{Type: "folder", ID: rid, Name: name, FileName: r.Name})
		}
		// With a single shared folder, skip the extra level.
		if len(l.Roots) == 1 {
			return l.Browse(makeID(0, ""))
		}
		return out, nil
	}
	n, rel, abs, err := l.Resolve(id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := &Listing{ID: id, Title: l.Roots[n].Name, Items: []Item{}}
	if t, ok := l.Names.Get(id); ok {
		out.Title = t
	}
	if rel != "" {
		out.Title = l.display(id, path.Base(rel))
		parent := path.Dir(rel)
		if parent == "." {
			parent = ""
		}
		if parent == "" && len(l.Roots) == 1 {
			out.ParentID = ""
		} else {
			out.ParentID = makeID(n, parent)
		}
	}

	var folders, videos []Item
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
			continue
		}
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.IsDir() || (info.Mode()&os.ModeSymlink != 0 && isDir(filepath.Join(abs, name))) {
			if isSubsFolder(name) {
				continue
			}
			fid := makeID(n, childRel)
			folders = append(folders, Item{Type: "folder", ID: fid, Name: l.display(fid, name), FileName: name})
			continue
		}
		mime, ok := videoExts[strings.ToLower(filepath.Ext(name))]
		if !ok || isSample(name) {
			continue
		}
		vid := makeID(n, childRel)
		videos = append(videos, Item{
			Type:      "video",
			ID:        vid,
			Name:      l.display(vid, strings.TrimSuffix(name, filepath.Ext(name))),
			FileName:  name,
			Size:      info.Size(),
			Modified:  info.ModTime(),
			MimeType:  mime,
			StreamURL: "/stream/" + vid,
		})
	}
	sortItems(folders)
	sortItems(videos)
	for i := range videos {
		_, vrel, vabs, _ := l.Resolve(videos[i].ID)
		videos[i].Subtitles = l.findSidecars(n, vrel, vabs, len(videos) == 1)
	}
	out.Items = append(folders, videos...)
	if out.Items == nil {
		out.Items = []Item{}
	}
	return out, nil
}

// Video returns a single video's metadata.
func (l *Library) Video(id string) (*Item, error) {
	n, rel, abs, err := l.Resolve(id)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	mime, ok := videoExts[strings.ToLower(filepath.Ext(abs))]
	if !ok || info.IsDir() {
		return nil, errBadID
	}
	only := countVideos(filepath.Dir(abs)) == 1
	name := filepath.Base(abs)
	return &Item{
		Type: "video", ID: id, Name: l.display(id, strings.TrimSuffix(name, filepath.Ext(name))), FileName: name,
		Size: info.Size(), Modified: info.ModTime(), MimeType: mime,
		StreamURL: "/stream/" + id,
		Subtitles: l.findSidecars(n, rel, abs, only),
	}, nil
}

func countVideos(dir string) int {
	entries, _ := os.ReadDir(dir)
	c := 0
	for _, e := range entries {
		if _, ok := videoExts[strings.ToLower(filepath.Ext(e.Name()))]; ok && !e.IsDir() && !isSample(e.Name()) {
			c++
		}
	}
	return c
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isSubsFolder(name string) bool {
	n := strings.ToLower(name)
	return n == "subs" || n == "subtitles" || n == "sub"
}

func isSample(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	return n == "sample" || strings.HasSuffix(n, "-sample") || strings.HasSuffix(n, ".sample")
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool { return naturalLess(items[i].Name, items[j].Name) })
}

// naturalLess compares strings case-insensitively, treating digit runs as numbers
// so "Episode 2" sorts before "Episode 10".
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ca, cb := a[0], b[0]
		if isDigit(ca) && isDigit(cb) {
			na, ra := takeDigits(a)
			nb, rb := takeDigits(b)
			ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(ta) != len(tb) {
				return len(ta) < len(tb)
			}
			if ta != tb {
				return ta < tb
			}
			a, b = ra, rb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func takeDigits(s string) (string, string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}
