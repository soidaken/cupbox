package decoder

import (
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// fixture is a tiny file committed in testdata/media, made by generate.ps1
// next to it: the minimized FFmpeg Cupbox ships decodes every one of them.
func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "testdata", "media", name)
}

func TestFrameNavigation(t *testing.T) {
	for _, name := range []string{"h264_aac.mp4", "h264_1080.mp4", "h264.mkv", "vfr.mp4"} {
		t.Run(name, func(t *testing.T) {
			d, _, err := Open(fixture(t, name))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			var pts []time.Duration
			var hashes [][32]byte
			for len(pts) < 40 {
				f, err := d.Next()
				if errors.Is(err, ErrEOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				pts = append(pts, f.PTS)
				hashes = append(hashes, sha256.Sum256(f.RGBA))
			}
			if len(pts) < 10 {
				t.Fatal("not enough fixture frames")
			}
			if name == "vfr.mp4" && pts[2]-pts[1] == pts[3]-pts[2] {
				t.Fatal("fixture must have unequal frame intervals")
			}
			check := func(f *Frame, err error, index int) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				if f.PTS != pts[index] || sha256.Sum256(f.RGBA) != hashes[index] {
					t.Fatalf("frame %d: want %v and its pixels, got %v", index, pts[index], f.PTS)
				}
			}
			for _, index := range []int{1, 5, len(pts) - 1} {
				f, err := d.FrameBefore(pts[index])
				check(f, err, index-1)
				f, err = d.SeekTo(pts[index-1] + time.Millisecond)
				check(f, err, index)
			}
			f, err := d.FrameBefore(pts[0])
			check(f, err, 0)
			if name == "vfr.mp4" {
				if _, err := d.SeekTo(pts[len(pts)-1] + time.Millisecond); !errors.Is(err, ErrEOF) {
					t.Fatalf("end boundary: %v", err)
				}
				f, err := d.FrameBefore(pts[len(pts)-1])
				check(f, err, len(pts)-2)
			}
		})
	}
}

func TestVideoMetadata(t *testing.T) {
	d, info, err := Open(fixture(t, "h264_aac.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// The values ffprobe reports for the fixture in testdata/media.
	if info.Width != 320 || info.Height != 180 || info.FPS != 30 || !info.HasAudio ||
		info.VideoBitRate != 369868 || info.AudioBitRate != 63511 {
		t.Fatalf("metadata differs from ffprobe: %+v", info)
	}
	silent, info, err := Open(fixture(t, "vfr.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	if info.HasAudio || info.AudioBitRate != 0 {
		t.Fatalf("silent video: %+v", info)
	}
}

// Exercise real headers, first frames and seeking: the MP4 startup fast
// path must preserve playback, with probing still available for other formats.
func TestPlaybackFixtures(t *testing.T) {
	for _, name := range []string{
		"h264_aac.mp4", "h264_1080.mp4", "hevc.mp4", "movtext.mp4", "h264.mkv",
		"mpeg2.ts", "mpeg4.avi", "vp8.webm", "wmv.wmv", "flv.flv", "ass.mkv", "srt.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			d, info, err := Open(fixture(t, name))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if info.Width <= 0 || info.Height <= 0 || info.Duration <= 0 {
				t.Fatalf("invalid metadata: %+v", info)
			}
			f, err := d.Next()
			if err != nil {
				t.Fatal(err)
			}
			if len(f.RGBA) != info.Width*info.Height*4 {
				t.Fatal("invalid frame buffer")
			}
			startPTS := f.PTS // MPEG-TS can have a nonzero stream start.
			target := min(time.Second, info.Duration/2)
			f, err = d.SeekTo(target)
			if err != nil {
				t.Fatal(err)
			}
			if f.PTS < target || f.PTS > max(target, startPTS)+250*time.Millisecond {
				t.Fatalf("seek %v landed at %v", target, f.PTS)
			}
		})
	}
}

func TestMP4Audio(t *testing.T) {
	a, err := OpenAudio(fixture(t, "h264_aac.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	buf := make([]float32, 4096)
	if n, err := a.Read(buf); err != nil || n == 0 {
		t.Fatalf("first audio: %d, %v", n, err)
	}
	if err := a.SetPosition(SampleRate); err != nil {
		t.Fatal(err)
	}
	if n, err := a.Read(buf); err != nil || n == 0 {
		t.Fatalf("audio after seek: %d, %v", n, err)
	}
}
