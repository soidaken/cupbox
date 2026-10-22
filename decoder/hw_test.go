package decoder

import (
	"errors"
	"testing"
	"time"
)

func TestHWUse(t *testing.T) {
	cases := []struct {
		name                string
		mode                hwMode
		preferred           bool
		width, height       int
		maxWidth, maxHeight int
		refused             bool
		want                bool
	}{
		{"auto, as shipped: the CPU decodes", hwAuto, false, 1920, 1080, 8192, 8192, false, false},
		{"auto, preferred: ordinary 1080p", hwAuto, true, 1920, 1080, 8192, 8192, false, true},
		{"auto, preferred: 4K on a device that stops at 4K", hwAuto, true, 3840, 2160, 4096, 4096, false, true},
		{"auto, preferred: too wide for an old device", hwAuto, true, 7680, 4320, 4096, 4096, false, false},
		{"auto, preferred: too tall for an old device", hwAuto, true, 3840, 2160, 8192, 2048, false, false},
		{"auto, preferred: the device says nothing about its limits", hwAuto, true, 7680, 4320, 0, 0, false, true},
		{"auto, preferred: refused here before", hwAuto, true, 1920, 1080, 8192, 8192, true, false},
		{"off, whatever the stream", hwOff, true, 1920, 1080, 8192, 8192, false, false},
		{"off even when it fits badly", hwOff, true, 7680, 4320, 4096, 4096, false, false},
		{"on, asked for by hand", hwOn, false, 1920, 1080, 8192, 8192, false, true},
		{"on, over the limits, by hand", hwOn, false, 7680, 4320, 4096, 4096, false, true},
		{"on, after a failure, by hand", hwOn, false, 1920, 1080, 8192, 8192, true, true},
	}
	for _, c := range cases {
		if got := hwUse(c.mode, c.preferred, c.width, c.height, c.maxWidth, c.maxHeight, c.refused); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The device is used when the machine has one that decodes the stream, and
// the CPU otherwise: both paths have to produce frames in order.
func TestHardwareOrCPU(t *testing.T) {
	for _, mode := range []string{"off", "on"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CUPBOX_HW", mode)
			d, info, err := Open(fixture(t, "h264_aac.mp4"))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if mode == "off" && d.HW() {
				t.Fatal("CUPBOX_HW=off still used the device")
			}
			if mode == "off" && info.HW != "" {
				t.Fatalf("Info.HW = %q with the device off", info.HW)
			}
			if mode == "on" && d.HW() && info.HW != "d3d11va" {
				t.Fatalf("Info.HW = %q while the device decodes", info.HW)
			}
			var last int64 = -1
			for i := 0; i < 20; i++ {
				f, err := d.Next()
				if err != nil {
					t.Fatalf("frame %d: %v", i, err)
				}
				if len(f.RGBA) != info.Width*info.Height*4 {
					t.Fatalf("frame %d: %d bytes", i, len(f.RGBA))
				}
				if int64(f.PTS) <= last {
					t.Fatalf("frame %d: PTS %v after %v", i, f.PTS, last)
				}
				last = int64(f.PTS)
			}
		})
	}
}

// A stream the driver will not decode (here H.264 4:4:4, which D3D11VA
// decoders do not take) must fall back to the CPU by itself, and keep
// playing from where it was.
func TestHardwareFallback(t *testing.T) {
	t.Setenv("CUPBOX_HW", "on")
	d, info, err := Open(fixture(t, "h264_444.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	first, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.RGBA) != info.Width*info.Height*4 {
		t.Fatal("no picture")
	}
	// Whatever the machine did, decoding continues and stays in order.
	last := first.PTS
	for i := 0; i < 5; i++ {
		f, err := d.Next()
		if errors.Is(err, ErrEOF) {
			break
		}
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if f.PTS <= last {
			t.Fatalf("frame %d: PTS %v after %v", i, f.PTS, last)
		}
		last = f.PTS
	}
	if d.HW() {
		t.Log("this device decodes 4:4:4; the fallback was not needed")
	}
}

// Moving a stream to the CPU mid-file — what playback does when the device
// stops handing frames over — keeps the picture going from where it was.
func TestSwitchCPUWhilePlaying(t *testing.T) {
	t.Setenv("CUPBOX_HW", "on")
	d, info, err := Open(fixture(t, "h264_aac.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.HW() {
		t.Skip("this machine has no D3D11VA device: nothing to switch from")
	}
	var last time.Duration
	for i := 0; i < 5; i++ {
		f, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		last = f.PTS
	}
	if err := d.switchCPU(); err != nil {
		t.Fatal(err)
	}
	if d.HW() {
		t.Fatal("still decoding on the device after switching")
	}
	if d.Info().HW != "" {
		t.Fatalf("Info.HW = %q after switching", d.Info().HW)
	}
	for i := 0; i < 10; i++ {
		f, err := d.Next()
		if err != nil {
			t.Fatalf("frame %d after the switch: %v", i, err)
		}
		if len(f.RGBA) != info.Width*info.Height*4 {
			t.Fatalf("frame %d after the switch: %d bytes", i, len(f.RGBA))
		}
		if f.PTS < last {
			t.Fatalf("frame %d went back to %v from %v", i, f.PTS, last)
		}
		last = f.PTS
	}
}
