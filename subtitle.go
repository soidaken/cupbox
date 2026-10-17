package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// A cue is one line of a subtitle file, and when it shows.
type cue struct {
	start, end time.Duration
	text       string
}

// subtitles are the cues of a file, in the order they show.
type subtitles struct {
	cues []cue
}

// loadSubtitles finds the subtitle file next to the video that has the same
// name: /films/a.mp4 with /films/a.srt. Files written in the local code page
// rather than UTF-8, which Chinese subtitle files often are, decode anyway.
func loadSubtitles(video string) *subtitles {
	base := strings.TrimSuffix(video, filepath.Ext(video))
	for _, name := range []string{base + ".srt", base + ".SRT", base + ".Srt"} {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		if s := parseSRT(data); len(s.cues) > 0 {
			logf_("subtitles %s: %d cues", name, len(s.cues))
			return s
		}
	}
	return nil
}

// parseSRT reads SubRip cues: a number, "00:00:01,000 --> 00:00:04,000",
// then the lines of text, then a blank line.
func parseSRT(data []byte) *subtitles {
	if !utf8.Valid(data) {
		// Not UTF-8: most likely GB18030, which covers GBK and GB2312.
		if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
			data = decoded
		}
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var (
		cues  []cue
		cur   cue
		lines []string
		open  bool
	)
	flush := func() {
		if open && len(lines) > 0 {
			cur.text = cleanCue(lines)
			if cur.text != "" && cur.end > cur.start {
				cues = append(cues, cur)
			}
		}
		lines, open = nil, false
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "-->") {
			flush()
			start, end, ok := parseCueTimes(trimmed)
			if ok {
				cur = cue{start: start, end: end}
				open = true
			}
			continue
		}
		if !open {
			continue // the cue's number, or blank lines between cues
		}
		if trimmed == "" {
			flush()
			continue
		}
		lines = append(lines, trimmed)
	}
	flush()
	sort.Slice(cues, func(i, j int) bool { return cues[i].start < cues[j].start })
	return &subtitles{cues: cues}
}

func parseCueTimes(s string) (start, end time.Duration, ok bool) {
	parts := strings.Split(s, "-->")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok1 := parseSRTTime(parts[0])
	end, ok2 := parseSRTTime(parts[1])
	return start, end, ok1 && ok2
}

// parseSRTTime reads 00:00:01,000, or 0:00:01.000 from files that use a dot.
func parseSRTTime(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	s = strings.Replace(s, ",", ".", 1)
	// Some files add position after the time, as in "00:00:01,000 X1:040".
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	hours, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	minutes, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	seconds, err3 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	d := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds*float64(time.Second))
	return d, true
}

// cleanCue strips the markup and positioning tags that SubRip files carry,
// keeping the text and its line breaks.
func cleanCue(lines []string) string {
	var out []string
	for _, line := range lines {
		var b strings.Builder
		inTag := false
		for _, r := range line {
			switch {
			case r == '<':
				inTag = true
			case r == '>':
				inTag = false
			case r == '{':
				inTag = true
			case r == '}':
				inTag = false
			case !inTag:
				b.WriteRune(r)
			}
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}

// at returns the text showing at t, or "".
func (s *subtitles) at(t time.Duration) string {
	if s == nil || len(s.cues) == 0 {
		return ""
	}
	i := sort.Search(len(s.cues), func(i int) bool { return s.cues[i].start > t }) - 1
	if i >= 0 && t < s.cues[i].end {
		return s.cues[i].text
	}
	return ""
}
