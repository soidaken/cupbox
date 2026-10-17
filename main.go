// Cupbox: a video player with no chrome, no buttons, and nothing but the
// picture.
package main

import (
	"fmt"
	"image"
	"os"
	"syscall"
	"time"
	"unsafe"

	"cupbox/decoder"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/plugins/game"
	"github.com/egoist/mygo/ui"
)

const barHeight = 32

var t0 = time.Now()

var logf *os.File

func logf_(format string, a ...any) {
	if logf == nil {
		name := os.Getenv("CUPBOX_LOG")
		if name == "" {
			return
		}
		f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		logf = f
	}
	fmt.Fprintf(logf, format+"\n", a...)
}

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

	p := &player{dec: dec, info: info, volume: 1}
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

	// Audio is the clock: the picture follows what is heard.
	if ad, err := decoder.OpenAudio(path); err == nil {
		defer ad.Close()
		p.ap = audio.NewPlayer(ad)
		p.ap.Play()
	} else {
		fmt.Fprintln(os.Stderr, "cupbox: no audio:", err)
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
			Content:        ui.View(func(c *ui.Context) { game.View(c, p).Fill() }),
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
	ap   *audio.Player
	win  *mygo.Window

	pending *decoder.Frame
	eof     bool
	paused  bool
	base    time.Duration
	started time.Time
	ready   bool
	shown   int
	dropped int

	volume float64

	// overlay
	bar      float32 // 0 hidden, 1 shown
	idle     time.Time
	dragging bool
	lastTap  time.Time
	keyHeld  bool
	keySince time.Time
	keyLast  time.Time
	lastX    float32
	lastY    float32
	lastW    float32
	lastH    float32

	ticks    int
	full     bool
	dragWin  bool
	dragFrom point
	winFrom  rect
}

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos  = user32.NewProc("SetWindowPos")
	procGetCursorPos  = user32.NewProc("GetCursorPos")
	procGetWindowRect = user32.NewProc("GetWindowRect")
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

func cursorPos() (point, bool) {
	var p point
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p, r != 0
}

func windowRect(hwnd uintptr) (rect, bool) {
	var r rect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func moveWindow(hwnd uintptr, x, y int32) {
	const swpNoSize, swpNoZOrder, swpNoActivate = 0x0001, 0x0004, 0x0010
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func (p *player) duration() time.Duration {
	if p.info.Duration > 0 {
		return p.info.Duration
	}
	return time.Hour
}

func (p *player) position() time.Duration {
	if p.paused || !p.ready {
		return p.base
	}
	if p.ap != nil {
		return p.ap.Position()
	}
	return p.base + time.Since(p.started)
}

func (p *player) setPaused(v bool) {
	if v == p.paused {
		return
	}
	if v {
		p.base = p.position()
		if p.ap != nil {
			p.ap.Pause()
		}
	} else {
		p.started = time.Now()
		if p.ap != nil {
			p.ap.Play()
		}
	}
	p.paused = v
}

// seekTo moves the picture and the sound to t.
func (p *player) seekTo(t time.Duration) {
	if t < 0 {
		t = 0
	}
	if d := p.duration(); t > d {
		t = d
	}
	logf_("seek to %v", t.Round(time.Millisecond))
	p.pending = nil
	p.eof = false
	p.base = t
	p.started = time.Now()
	if err := p.dec.Seek(t); err != nil {
		return
	}
	if p.ap != nil {
		p.ap.Seek(t)
	}
	p.pump(t, 12)
}

// pump shows decoded frames while they are due at pos.
func (p *player) pump(pos time.Duration, max int) {
	for i := 0; i < max && !p.eof; i++ {
		if p.pending == nil {
			f, err := p.dec.Next()
			if err != nil {
				p.eof = true
				return
			}
			p.pending = f
		}
		if p.pending.PTS > pos+10*time.Millisecond {
			return
		}
		if p.pending.PTS < pos-150*time.Millisecond {
			p.dropped++
			p.pending = nil
			continue
		}
		p.bm.Update(p.img)
		p.shown++
		p.pending = nil
	}
}

func (p *player) Update(in *game.Input) {
	now := time.Now()
	x, y, over := in.Pointer()
	w, h := in.Size()

	if in.Pressed(ui.KeyEscape) {
		mygo.App.Quit()
		return
	}
	if in.Pressed(ui.KeySpace) {
		p.setPaused(!p.paused)
	}

	p.ticks++
	if p.ticks%60 == 0 {
		logf_("tick x=%.0f y=%.0f over=%v focused=%v bar=%.2f", x, y, over, in.Focused(), p.bar)
	}

	// Arrow keys: a tap steps two seconds; holding it runs at double speed.
	step := func(k ui.Key, dir time.Duration) {
		if in.Pressed(k) {
			p.seekTo(p.position() + dir)
			p.keyHeld, p.keySince, p.keyLast = true, now, now
		} else if in.Down(k) && p.keyHeld && now.Sub(p.keySince) > 250*time.Millisecond &&
			now.Sub(p.keyLast) > 100*time.Millisecond {
			p.seekTo(p.position() + dir/10)
			p.keyLast = now
		} else if in.Released(k) {
			p.keyHeld = false
		}
	}
	step(ui.KeyLeft, -2*time.Second)
	step(ui.KeyRight, 2*time.Second)

	// Volume: the wheel, with nothing on screen to show it.
	if _, dy := in.Wheel(); dy != 0 && p.ap != nil {
		p.volume = min(max(p.volume+float64(dy)*0.02, 0), 1)
		p.ap.SetVolume(p.volume)
	}

	inBar := over && y >= h-barHeight
	if in.PointerPressed(0) {
		logf_("press x=%.0f y=%.0f over=%v inBar=%v win=%v full=%v", x, y, over, inBar, p.win != nil, p.full)
	}
	if in.PointerPressed(0) && !inBar && !p.full && p.win != nil {
		if c, ok := cursorPos(); ok {
			if r, ok := windowRect(p.win.NativeHandle()); ok {
				p.dragWin, p.dragFrom, p.winFrom = true, c, r
			}
		}
	}
	if p.dragWin {
		if in.PointerDown(0) {
			if c, ok := cursorPos(); ok {
				moveWindow(p.win.NativeHandle(),
					p.winFrom.Left+c.X-p.dragFrom.X, p.winFrom.Top+c.Y-p.dragFrom.Y)
			}
		} else {
			p.dragWin = false
		}
	}

	if in.PointerPressed(0) {
		if inBar {
			p.dragging = true
		} else if now.Sub(p.lastTap) < 400*time.Millisecond {
			if p.win != nil {
				p.win.ToggleFullScreen()
				p.full = !p.full
			}
		}
		p.lastTap = now
		p.idle = now
	}
	if p.dragging {
		if in.PointerDown(0) {
			p.seekTo(time.Duration(float64(x) / float64(w) * float64(p.duration())))
		} else {
			p.dragging = false
		}
	}

	// The bar and the clock show when the mouse moves, and fade after two
	// seconds of stillness.
	if over && (x != p.lastX || y != p.lastY) {
		p.idle = now
	}
	if p.dragging {
		p.idle = now
	}
	p.lastX, p.lastY, p.lastW, p.lastH = x, y, w, h
	target := float32(0)
	if now.Sub(p.idle) < 2*time.Second || p.dragging {
		target = 1
	}
	p.bar += (target - p.bar) * 0.25
	if p.bar < 0.01 {
		p.bar = 0
	}

	if p.paused || p.eof || !p.ready {
		return
	}
	p.pump(p.position(), 6)
}

func (p *player) Draw(pt *ui.Painter, r ui.Rect, alpha float64) {
	if !p.ready {
		p.ready = true
		p.started = time.Now()
		p.idle = time.Now()
		if ws := mygo.Windows(); len(ws) > 0 {
			p.win = ws[0]
		}
		fmt.Printf("first frame after %v\n", time.Since(t0).Round(time.Millisecond))
	}
	pt.Fill(r, ui.RGBA(0, 0, 0, 1), 0)
	pt.Image(p.bm, r, ui.Contain)

	if p.bar <= 0 {
		return
	}
	a := p.bar
	bar := ui.Rect{X: r.X, Y: r.Y + r.H - barHeight, W: r.W, H: barHeight}
	pt.Fill(bar, ui.RGBA(255, 255, 255, 0.35*a), 0)

	frac := float64(p.position()) / float64(p.duration())
	frac = min(max(frac, 0), 1)
	if frac > 0 {
		pt.Fill(ui.Rect{X: bar.X, Y: bar.Y, W: float32(frac) * bar.W, H: bar.H},
			ui.RGBA(255, 238, 150, 0.90*a), 0)
	}

	s := clock(p.position()) + " / " + clock(p.duration())
	col := ui.RGBA(255, 255, 255, 0.75*a)
	tw, th := pt.MeasureText(0, ui.Span{Text: s, Size: 13, Color: col})
	pt.Text(r.X+(r.W-tw)/2, bar.Y-th-8, s, 13, col)
}

// clock formats d as m:ss, or h:mm:ss when it is an hour or longer.
func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
