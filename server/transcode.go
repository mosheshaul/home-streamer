package main

// On-the-fly conversion with FFmpeg, for videos a device can't decode (for
// example 4K Dolby Vision HEVC on a phone) or that are too big to stream over
// the internet. Output is HLS: a fixed playlist of 6-second MPEG-TS segments,
// produced by one FFmpeg process that is restarted when the viewer seeks far.

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const segSeconds = 6

// FFmpeg locates the ffmpeg/ffprobe programs and the best H.264 encoder.
type FFmpeg struct {
	FFmpeg, FFprobe string
	Encoder         string
	Zscale          bool
	HWAccel         string
	CacheDir        string
}

func findFFmpeg(cfg *Config) *FFmpeg {
	exeName := func(n string) string {
		if runtime.GOOS == "windows" {
			return n + ".exe"
		}
		return n
	}
	var candidates []string
	if cfg.FFmpeg != "" {
		candidates = append(candidates, cfg.FFmpeg)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, exeName("ffmpeg")), filepath.Join(dir, "ffmpeg", "bin", exeName("ffmpeg")))
	}
	if p, err := exec.LookPath(exeName("ffmpeg")); err == nil {
		candidates = append(candidates, p)
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		// winget installs a shim here; it may not be on PATH until the next sign-in.
		candidates = append(candidates, filepath.Join(la, "Microsoft", "WinGet", "Links", "ffmpeg.exe"))
	}
	for _, c := range candidates {
		st, err := os.Stat(c)
		if err != nil || st.IsDir() {
			continue
		}
		probe := filepath.Join(filepath.Dir(c), exeName("ffprobe"))
		if _, err := os.Stat(probe); err != nil {
			if p, err := exec.LookPath(exeName("ffprobe")); err == nil {
				probe = p
			} else {
				continue
			}
		}
		f := &FFmpeg{FFmpeg: c, FFprobe: probe, HWAccel: cfg.HWAccel}
		if f.HWAccel == "" {
			f.HWAccel = "auto"
		}
		cache, err := os.UserCacheDir()
		if err != nil {
			cache = os.TempDir()
		}
		f.CacheDir = filepath.Join(cache, "HomeStreamer")
		os.RemoveAll(filepath.Join(f.CacheDir, "hls")) // leftovers from a previous run
		f.detect(cfg.Encoder)
		return f
	}
	return nil
}

func (f *FFmpeg) detect(preferred string) {
	out, _ := exec.Command(f.FFmpeg, "-hide_banner", "-filters").Output()
	f.Zscale = strings.Contains(string(out), " zscale ") && strings.Contains(string(out), " tonemap ")
	encoders := []string{"h264_nvenc", "h264_qsv", "h264_amf"}
	if preferred != "" {
		encoders = []string{preferred}
	}
	for _, e := range encoders {
		if e == "libx264" {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := exec.CommandContext(ctx, f.FFmpeg, "-v", "error", "-f", "lavfi", "-i", "color=black:s=1280x720:d=0.5",
			"-c:v", e, "-f", "null", "-").Run()
		cancel()
		if err == nil {
			f.Encoder = e
			return
		}
	}
	f.Encoder = "libx264"
}

// ---------- probing ----------

type AudioTrack struct {
	Index    int    `json:"index"` // position among audio streams
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

type EmbeddedSub struct {
	Index    int    `json:"index"` // position among subtitle streams
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Label    string `json:"label"`
	Forced   bool   `json:"forced,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Text     bool   `json:"text"`
	Ready    bool   `json:"ready"`
	URL      string `json:"url,omitempty"`
}

type MediaInfo struct {
	Duration     float64       `json:"duration"`
	Bitrate      int64         `json:"bitrate"`
	VideoCodec   string        `json:"videoCodec"`
	Width        int           `json:"width"`
	Height       int           `json:"height"`
	HDR          bool          `json:"hdr"`
	DolbyVision  bool          `json:"dolbyVision"`
	Audio        []AudioTrack  `json:"audio"`
	Subtitles    []EmbeddedSub `json:"embeddedSubtitles"`
	CanTranscode bool          `json:"canTranscode"`
	// SubsProgress is the extraction progress in percent while it runs, else -1.
	SubsProgress int `json:"subtitlesProgress"`
}

type probeJSON struct {
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		CodecType      string            `json:"codec_type"`
		CodecName      string            `json:"codec_name"`
		Width          int               `json:"width"`
		Height         int               `json:"height"`
		Channels       int               `json:"channels"`
		ColorTransfer  string            `json:"color_transfer"`
		Tags           map[string]string `json:"tags"`
		Disposition    map[string]int    `json:"disposition"`
		SideDataList   []map[string]any  `json:"side_data_list"`
		CodecTagString string            `json:"codec_tag_string"`
	} `json:"streams"`
}

var textSubCodecs = map[string]bool{"subrip": true, "srt": true, "ass": true, "ssa": true, "mov_text": true, "webvtt": true, "text": true}

type probeCache struct {
	mu sync.Mutex
	m  map[string]*MediaInfo
}

var probes = probeCache{m: map[string]*MediaInfo{}}

func fileKey(abs string) string {
	st, err := os.Stat(abs)
	if err != nil {
		return abs
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", abs, st.Size(), st.ModTime().UnixNano())))
	return hex.EncodeToString(h[:10])
}

func (f *FFmpeg) Probe(abs string) (*MediaInfo, error) {
	key := fileKey(abs)
	probes.mu.Lock()
	if mi, ok := probes.m[key]; ok {
		probes.mu.Unlock()
		return mi, nil
	}
	probes.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, f.FFprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", abs).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var pj probeJSON
	if err := json.Unmarshal(out, &pj); err != nil {
		return nil, err
	}
	mi := &MediaInfo{CanTranscode: true, Audio: []AudioTrack{}, Subtitles: []EmbeddedSub{}}
	mi.Duration, _ = strconv.ParseFloat(pj.Format.Duration, 64)
	mi.Bitrate, _ = strconv.ParseInt(pj.Format.BitRate, 10, 64)
	na, ns := 0, 0
	for _, s := range pj.Streams {
		lang := normalizeLang(s.Tags["language"])
		switch s.CodecType {
		case "video":
			if mi.VideoCodec != "" || s.Disposition["attached_pic"] == 1 {
				continue
			}
			mi.VideoCodec, mi.Width, mi.Height = s.CodecName, s.Width, s.Height
			mi.HDR = s.ColorTransfer == "smpte2084" || s.ColorTransfer == "arib-std-b67"
			for _, sd := range s.SideDataList {
				if t, _ := sd["side_data_type"].(string); strings.Contains(strings.ToLower(t), "dovi") {
					mi.DolbyVision = true
				}
			}
		case "audio":
			mi.Audio = append(mi.Audio, AudioTrack{Index: na, Codec: s.CodecName, Channels: s.Channels,
				Language: lang, Title: s.Tags["title"], Default: s.Disposition["default"] == 1})
			na++
		case "subtitle":
			es := EmbeddedSub{Index: ns, Codec: s.CodecName, Language: lang, Title: s.Tags["title"],
				Forced: s.Disposition["forced"] == 1, Default: s.Disposition["default"] == 1, Text: textSubCodecs[s.CodecName]}
			es.Label = langNames[lang]
			if es.Label == "" {
				es.Label = strings.ToUpper(lang)
			}
			if es.Label == "" {
				es.Label = fmt.Sprintf("Track %d", ns+1)
			}
			if es.Title != "" && !strings.EqualFold(es.Title, es.Label) {
				es.Label += " (" + es.Title + ")"
			}
			if es.Forced {
				es.Label += " (forced)"
			}
			mi.Subtitles = append(mi.Subtitles, es)
			ns++
		}
	}
	if mi.Bitrate == 0 {
		if st, err := os.Stat(abs); err == nil && mi.Duration > 0 {
			mi.Bitrate = int64(float64(st.Size()*8) / mi.Duration)
		}
	}
	probes.mu.Lock()
	probes.m[key] = mi
	probes.mu.Unlock()
	return mi, nil
}

func normalizeLang(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if l == "" || l == "und" {
		return ""
	}
	if code, ok := langTags[l]; ok {
		return code
	}
	if i := strings.IndexAny(l, "-_"); i > 0 {
		return normalizeLang(l[:i])
	}
	return l
}

// ---------- embedded subtitle extraction ----------

// In converted playback the player can't read subtitles from the original
// file, so text subtitle tracks are extracted once to SRT files and cached.

type subJob struct {
	done     chan struct{}
	err      error
	progress float64 // 0..1, guarded by subJobsMu
}

var (
	subJobsMu sync.Mutex
	subJobs   = map[string]*subJob{}
	subSem    = make(chan struct{}, 1)
)

func (f *FFmpeg) subDir(abs string) string {
	return filepath.Join(f.CacheDir, "subs", fileKey(abs))
}

// SubsReady reports whether extraction finished, starting it if needed.
func (f *FFmpeg) SubsReady(abs string, mi *MediaInfo) bool {
	dir := f.subDir(abs)
	if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
		return true
	}
	var text []int
	for _, s := range mi.Subtitles {
		if s.Text {
			text = append(text, s.Index)
		}
	}
	if len(text) == 0 {
		return true
	}
	subJobsMu.Lock()
	job, running := subJobs[dir]
	if !running {
		job = &subJob{done: make(chan struct{})}
		subJobs[dir] = job
		go func() {
			subSem <- struct{}{}
			defer func() { <-subSem }()
			job.err = f.extractSubs(abs, dir, text, mi.Duration, job)
			if job.err != nil {
				log.Printf("subtitle extraction failed for %s: %v", filepath.Base(abs), job.err)
			}
			close(job.done)
			subJobsMu.Lock()
			delete(subJobs, dir)
			subJobsMu.Unlock()
		}()
	}
	subJobsMu.Unlock()
	return false
}

// SubsProgress returns how far extraction is (0..1), or -1 if it isn't running.
func (f *FFmpeg) SubsProgress(abs string) float64 {
	subJobsMu.Lock()
	defer subJobsMu.Unlock()
	if j, ok := subJobs[f.subDir(abs)]; ok {
		return j.progress
	}
	return -1
}

func (f *FFmpeg) extractSubs(abs, dir string, idx []int, duration float64, job *subJob) error {
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	// The whole file has to be read to find every subtitle, so this takes
	// about as long as copying the file once.
	args := []string{"-v", "error", "-nostdin", "-y", "-progress", "pipe:1", "-nostats", "-i", abs}
	for _, i := range idx {
		args = append(args, "-map", fmt.Sprintf("0:s:%d", i), "-c:s", "srt", filepath.Join(tmp, fmt.Sprintf("%d.srt", i)))
	}
	start := time.Now()
	log.Printf("Extracting %d subtitle tracks from %s…", len(idx), filepath.Base(abs))
	cmd := exec.Command(f.FFmpeg, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "out_time_us="); ok && duration > 0 {
			if us, err := strconv.ParseFloat(v, 64); err == nil {
				subJobsMu.Lock()
				job.progress = math.Max(0, math.Min(0.99, us/1e6/duration))
				subJobsMu.Unlock()
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	os.WriteFile(filepath.Join(tmp, "done"), nil, 0o644)
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return err
	}
	log.Printf("Extracted %d subtitle tracks from %s in %s", len(idx), filepath.Base(abs), time.Since(start).Round(time.Second))
	return nil
}

func (f *FFmpeg) SubPath(abs string, index int) string {
	return filepath.Join(f.subDir(abs), fmt.Sprintf("%d.srt", index))
}

// ---------- HLS sessions ----------

type Quality struct {
	MaxW, MaxH int
	Kbps       int
	Audio      int
}

func (q Quality) key() string { return fmt.Sprintf("%dx%d@%d/a%d", q.MaxW, q.MaxH, q.Kbps, q.Audio) }

func parseQuality(get func(string) string) Quality {
	q := Quality{MaxW: 1920, MaxH: 1080, Kbps: 8000}
	if h, err := strconv.Atoi(get("h")); err == nil {
		switch {
		case h <= 480:
			q.MaxW, q.MaxH = 854, 480
		case h <= 720:
			q.MaxW, q.MaxH = 1280, 720
		default:
			q.MaxW, q.MaxH = 1920, 1080
		}
	}
	if b, err := strconv.Atoi(get("br")); err == nil && b >= 500 && b <= 40000 {
		q.Kbps = b
	}
	if a, err := strconv.Atoi(get("a")); err == nil && a >= 0 {
		q.Audio = a
	}
	return q
}

func (f *FFmpeg) Playlist(mi *MediaInfo, query string) string {
	var b strings.Builder
	n := int(math.Ceil(mi.Duration / segSeconds))
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n", segSeconds)
	for i := 0; i < n; i++ {
		d := math.Min(segSeconds, mi.Duration-float64(i*segSeconds))
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nseg/%d.ts?%s\n", d, i, query)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

type session struct {
	key      string
	abs      string
	q        Quality
	mi       *MediaInfo
	dir      string
	mu       sync.Mutex
	cond     *sync.Cond
	cmd      *exec.Cmd
	start    int          // first segment of the running process
	done     map[int]bool // finished segments on disk
	running  bool
	failed   error
	lastReq  int
	lastUsed time.Time
	gen      int
}

type sessions struct {
	mu sync.Mutex
	m  map[string]*session
	f  *FFmpeg
}

func newSessions(f *FFmpeg) *sessions {
	s := &sessions{m: map[string]*session{}, f: f}
	go s.reaper()
	return s
}

// reaper stops conversions nobody has asked for in a while.
func (s *sessions) reaper() {
	for range time.Tick(15 * time.Second) {
		s.mu.Lock()
		for k, ss := range s.m {
			ss.mu.Lock()
			idle := time.Since(ss.lastUsed) > 2*time.Minute
			ss.mu.Unlock()
			if idle {
				ss.stop()
				os.RemoveAll(ss.dir)
				delete(s.m, k)
			}
		}
		s.mu.Unlock()
	}
}

func (s *sessions) get(abs string, mi *MediaInfo, q Quality) *session {
	key := fileKey(abs) + "/" + q.key()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ss, ok := s.m[key]; ok {
		return ss
	}
	// One conversion per video: switching quality replaces the old one.
	prefix := fileKey(abs) + "/"
	for k, ss := range s.m {
		if strings.HasPrefix(k, prefix) || len(s.m) >= 3 {
			ss.stop()
			os.RemoveAll(ss.dir)
			delete(s.m, k)
		}
	}
	h := sha1.Sum([]byte(key))
	ss := &session{key: key, abs: abs, q: q, mi: mi, dir: filepath.Join(s.f.CacheDir, "hls", hex.EncodeToString(h[:8])),
		done: map[int]bool{}, lastUsed: time.Now()}
	ss.cond = sync.NewCond(&ss.mu)
	s.m[key] = ss
	return ss
}

// Segment returns the path of segment n, converting it if needed.
func (s *sessions) Segment(ctx context.Context, ss *session, n int) (string, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.lastUsed = time.Now()
	ss.lastReq = n
	path := filepath.Join(ss.dir, fmt.Sprintf("%d.ts", n))
	if ss.done[n] {
		ss.cleanupOld(n)
		return path, nil
	}
	// Restart unless the running process will reach n soon.
	ahead := ss.highestDone()
	if ss.failed != nil || !ss.running || n < ss.start || n > max(ahead, ss.start)+4 {
		if err := ss.launch(s.f, n); err != nil {
			return "", err
		}
	}
	deadline := time.Now().Add(90 * time.Second)
	stopWake := context.AfterFunc(ctx, func() { ss.mu.Lock(); ss.cond.Broadcast(); ss.mu.Unlock() })
	defer stopWake()
	timer := time.AfterFunc(time.Until(deadline), func() { ss.mu.Lock(); ss.cond.Broadcast(); ss.mu.Unlock() })
	defer timer.Stop()
	for !ss.done[n] {
		if ss.failed != nil {
			return "", ss.failed
		}
		if !ss.running {
			return "", errors.New("conversion stopped")
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if time.Now().After(deadline) {
			return "", errors.New("conversion is too slow on this PC")
		}
		ss.cond.Wait()
	}
	return path, nil
}

func (ss *session) highestDone() int {
	h := -1
	for k := range ss.done {
		if k > h {
			h = k
		}
	}
	return h
}

// cleanupOld deletes segments well behind the viewer to save disk space.
func (ss *session) cleanupOld(n int) {
	for k := range ss.done {
		if k < n-10 {
			os.Remove(filepath.Join(ss.dir, fmt.Sprintf("%d.ts", k)))
			delete(ss.done, k)
		}
	}
}

func (ss *session) stop() {
	ss.mu.Lock()
	cmd := ss.cmd
	ss.cmd = nil
	ss.running = false
	ss.gen++
	ss.cond.Broadcast()
	ss.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

// launch starts FFmpeg at segment n. Must be called with ss.mu held.
func (ss *session) launch(f *FFmpeg, n int) error {
	if ss.cmd != nil && ss.cmd.Process != nil {
		ss.cmd.Process.Kill()
	}
	ss.gen++
	gen := ss.gen
	ss.failed = nil
	ss.start = n
	for k := range ss.done {
		delete(ss.done, k)
	}
	os.RemoveAll(ss.dir)
	if err := os.MkdirAll(ss.dir, 0o755); err != nil {
		return err
	}
	list := filepath.Join(ss.dir, fmt.Sprintf("list-%d.csv", gen))
	args := f.transcodeArgs(ss.abs, ss.mi, ss.q, n, ss.dir, list)
	cmd := exec.Command(f.FFmpeg, args...)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("Converting %s from %s (%s, %s)", filepath.Base(ss.abs), fmtDur(n*segSeconds), ss.q.key(), f.Encoder)
	ss.cmd = cmd
	ss.running = true

	var tail []string
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			tail = append(tail, sc.Text())
			if len(tail) > 8 {
				tail = tail[1:]
			}
		}
	}()
	// Watch the segment list FFmpeg writes as each segment completes.
	go func() {
		seen := 0
		for {
			data, _ := os.ReadFile(list)
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			ss.mu.Lock()
			if ss.gen != gen {
				ss.mu.Unlock()
				return
			}
			for _, l := range lines[min(seen, len(lines)):] {
				name := strings.SplitN(l, ",", 2)[0]
				if k, err := strconv.Atoi(strings.TrimSuffix(name, ".ts")); err == nil {
					ss.done[k] = true
					seen++
				}
			}
			// Don't run too far ahead of the viewer: stop and resume later.
			if ss.highestDone()-ss.lastReq > 40 && ss.cmd != nil && ss.cmd.Process != nil {
				ss.cmd.Process.Kill()
			}
			finished := !ss.running
			ss.cond.Broadcast()
			ss.mu.Unlock()
			if finished {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
	go func() {
		err := cmd.Wait()
		time.Sleep(300 * time.Millisecond) // let the watcher read the final list entries
		data, _ := os.ReadFile(list)
		ss.mu.Lock()
		defer ss.mu.Unlock()
		if ss.gen != gen {
			return
		}
		for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if k, e := strconv.Atoi(strings.TrimSuffix(strings.SplitN(l, ",", 2)[0], ".ts")); e == nil {
				ss.done[k] = true
			}
		}
		ss.running = false
		ss.cmd = nil
		lastSeg := int(math.Ceil(ss.mi.Duration/segSeconds)) - 1
		if err != nil && ss.highestDone() < lastSeg && ss.highestDone()-ss.lastReq <= 40 {
			ss.failed = fmt.Errorf("ffmpeg: %v: %s", err, strings.Join(tail, " | "))
			log.Printf("Conversion failed: %v", ss.failed)
		}
		ss.cond.Broadcast()
	}()
	return nil
}

func (f *FFmpeg) transcodeArgs(abs string, mi *MediaInfo, q Quality, startSeg int, dir, list string) []string {
	start := startSeg * segSeconds
	args := []string{"-hide_banner", "-v", "error", "-nostdin", "-y"}
	if f.HWAccel != "none" {
		args = append(args, "-hwaccel", f.HWAccel)
	}
	if start > 0 {
		args = append(args, "-ss", strconv.Itoa(start))
	}
	args = append(args, "-i", abs, "-map", "0:v:0")
	if len(mi.Audio) > 0 {
		a := q.Audio
		if a >= len(mi.Audio) {
			a = 0
		}
		args = append(args, "-map", fmt.Sprintf("0:a:%d", a))
	}
	vf := fmt.Sprintf("scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2", q.MaxW, q.MaxH)
	if mi.HDR && f.Zscale {
		// HDR (including Dolby Vision's HDR10 base layer) to normal colors.
		vf += ",zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv"
	}
	vf += ",format=yuv420p"
	br := q.Kbps
	args = append(args, "-vf", vf, "-c:v", f.Encoder,
		"-b:v", fmt.Sprintf("%dk", br), "-maxrate", fmt.Sprintf("%dk", br*3/2), "-bufsize", fmt.Sprintf("%dk", br*2),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segSeconds))
	switch f.Encoder {
	case "libx264":
		args = append(args, "-preset", "veryfast", "-profile:v", "high", "-sc_threshold", "0")
	case "h264_nvenc":
		args = append(args, "-preset", "p4", "-profile:v", "high", "-forced-idr", "1")
	case "h264_qsv":
		args = append(args, "-preset", "faster", "-profile:v", "high")
	case "h264_amf":
		args = append(args, "-quality", "speed", "-profile:v", "high")
	}
	args = append(args,
		"-c:a", "aac", "-ac", "2", "-b:a", "192k",
		"-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
		"-max_muxing_queue_size", "4096",
		"-output_ts_offset", strconv.Itoa(start),
		"-f", "segment", "-segment_time", strconv.Itoa(segSeconds), "-segment_format", "mpegts",
		"-segment_start_number", strconv.Itoa(startSeg),
		"-segment_list", list, "-segment_list_type", "csv",
		filepath.Join(dir, "%d.ts"))
	return args
}

func fmtDur(sec int) string {
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}
