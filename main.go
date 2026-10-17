// Cupbox: a video player with no chrome, no buttons, and nothing but the
// picture.
package main

import (
	"fmt"
	"image"
	"os"
	"time"

	"cupbox/decoder"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/game"
	"github.com/egoist/mygo/ui"
)

var t0 = time.Now()

func main() {
	if len(os.Args) < 2 {
		fail("usage: cupbox <file>")
	}
	path := os.Args[1]

	dec, info, err := decoder.Open(path)
	if err != nil {
		fail(err.Error())
	}
	defer dec.Close()
	if info.Width == 0 || info.Height == 0 {
		fail("no video")
	}

	p := &player{dec: dec, info: info}
	p.img = &image.RGBA{
		Pix:    dec.Pixels(),
		Stride: info.Width * 4,
		Rect:   image.Rect(0, 0, info.Width, info.Height),
	}
	p.bm = ui.NewBitmap(p.img)

	// Decode the first frame before the window exists: it shows the picture
	// the moment it appears, and the cost is paid while the window is made.
	if _, err := dec.Next(); err == nil {
		p.bm.Update(p.img)
	} else {
		fail("cannot decode: " + err.Error())
	}

	mygo.App.WhenReady(func() {
		w, h := fitToScreen(info.Width, info.Height)
		mygo.NewWindow(mygo.WindowOptions{
			Title:          "Cupbox",
			Width:          w,
			Height:         h,
			UseContentSize: true,
			Frameless:      true,
			DisableResize:  true,
			Content:        ui.View(func(c *ui.Context) { game.View(c, p) }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "cupbox:", msg)
	os.Exit(1)
}

// fitToScreen returns the window size for a video of w×h: its own pixels,
// shrunk to nine tenths of the work area when it would not fit.
func fitToScreen(w, h int) (int, int) {
	ds := mygo.Screen.Displays()
	if len(ds) == 0 {
		return w, h
	}
	d := ds[0]
	scale := d.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	maxW := int(float64(d.WorkArea.Width) / scale * 0.9)
	maxH := int(float64(d.WorkArea.Height) / scale * 0.9)
	if w <= maxW && h <= maxH {
		return w, h
	}
	r := min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return int(float64(w) * r), int(float64(h) * r)
}

type player struct {
	dec  *decoder.Decoder
	info decoder.Info
	img  *image.RGBA
	bm   *ui.Bitmap

	pending *decoder.Frame
	eof     bool
	paused  bool
	base    time.Duration
	started time.Time
	ready   bool
	shown   int
}

func (p *player) position() time.Duration {
	if p.paused || !p.ready {
		return p.base
	}
	return p.base + time.Since(p.started)
}

func (p *player) setPaused(v bool) {
	if v == p.paused {
		return
	}
	if v {
		p.base = p.position()
	} else {
		p.started = time.Now()
	}
	p.paused = v
}

func (p *player) present(f *decoder.Frame) {
	p.bm.Update(p.img)
	p.shown++
}

func (p *player) Update(in *game.Input) {
	if in.Pressed(ui.KeyEscape) {
		mygo.App.Quit()
		return
	}
	if in.Pressed(ui.KeySpace) {
		p.setPaused(!p.paused)
	}
	if p.paused || p.eof || !p.ready {
		return
	}
	pos := p.position()
	for i := 0; i < 4 && !p.eof; i++ {
		if p.pending == nil {
			f, err := p.dec.Next()
			if err != nil {
				p.eof = true
				fmt.Printf("played %d frames in %v\n", p.shown, time.Since(t0).Round(time.Millisecond))
				return
			}
			p.pending = f
		}
		if p.pending.PTS > pos {
			return
		}
		p.present(p.pending)
		p.pending = nil
	}
}

func (p *player) Draw(pt *ui.Painter, r ui.Rect, alpha float64) {
	if !p.ready {
		p.ready = true
		p.started = time.Now()
		fmt.Printf("first frame after %v\n", time.Since(t0).Round(time.Millisecond))
	}
	pt.Image(p.bm, r, ui.FillBox)
}
