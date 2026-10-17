// Cupbox: a video player with no chrome, no buttons, and nothing but the
// picture.
package main

import (
	"fmt"
	"image"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"cupbox/decoder"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"
)

const (
	barHeight  = 32
	emptyW     = 960
	emptyH     = 640
	escConfirm = 2 * time.Second
	dragSlop   = 4 // pixels a press may wander before it moves the window
)

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
	var (
		p    *player
		winW = emptyW
		winH = emptyH
	)
	if len(os.Args) < 2 {
		p = &player{empty: true}
		p.in = newInput()
	} else {
		path := os.Args[1]
		dec, info, err := decoder.Open(path)
		if err != nil {
			fail(err.Error())
		}
		if info.Width == 0 || info.Height == 0 {
			fail("no video")
		}
		p = &player{dec: dec, info: info, volume: 1, in: newInput()}
		p.img = &image.RGBA{
			Pix:    dec.Pixels(),
			Stride: info.Width * 4,
			Rect:   image.Rect(0, 0, info.Width, info.Height),
		}
		p.bm = ui.NewBitmap(p.img)

		// Decode the first frame before the window exists: it shows the
		// picture the moment it appears, and the cost is paid while the
		// window is made.
		if _, err := dec.Next(); err != nil {
			fail("cannot decode: " + err.Error())
		}
		p.bm.Update(p.img)

		p.subs = loadSubtitles(path)

		// Audio is the clock: the picture follows what is heard.
		if ad, err := decoder.OpenAudio(path); err == nil {
			p.ad = ad
			p.ap = audio.NewPlayer(ad)
			p.ap.Play()
		} else {
			logf_("no audio: %v", err)
		}
		winW, winH = info.Width, info.Height
	}

	if !mygo.App.RequestSingleInstanceLock() {
		return // another Cupbox is running: it got our command line
	}
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		if len(args) > 0 {
			p.requestOpen(args[0])
		}
	})

	mygo.App.WhenReady(func() {
		w, h := winW, winH
		if p.dec != nil {
			w, h = fitToScreen(p.info.Width, p.info.Height)
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "Cupbox",
			Width:          w,
			Height:         h,
			UseContentSize: true,
			Frameless:      true,
			DisableResize:  true,
			Content:        ui.View(p.view),
		})
		win.OnFileDrop(func(e *mygo.FileDropEvent) {
			if len(e.Paths) > 0 {
				p.requestOpen(e.Paths[0])
			}
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

// Win32 window control: dragging, fullscreen on the current monitor, and
// asking which monitor that is.
var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos    = user32.NewProc("SetWindowPos")
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procMonitorFromWin  = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW = user32.NewProc("GetMonitorInfoW")
	procGetWindowLong   = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLong   = user32.NewProc("SetWindowLongPtrW")
	dwmapi              = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	swpNoSize       = 0x0001
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpShowWindow   = 0x0040
	hwndTop         = ^uintptr(0) // (HWND)-1
	hwndNotTop      = ^uintptr(1) // (HWND)-2
	swpFrameChanged = 0x0020

	gwlStyle     = -16
	wsCaption    = 0x00C00000
	wsThickFrame = 0x00040000

	dwmwaCornerPreference = 33
	dwmwaBorderColor      = 34
	dwmcpDoNotRound       = 1
	dwmcpDefault          = 0
	dwmColorNone          = 0xFFFFFFFE
	dwmColorDefault       = 0xFFFFFFFF
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

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

func setWindowPos(hwnd, after uintptr, x, y, cx, cy int32, flags uint32) {
	procSetWindowPos.Call(hwnd, after, uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), uintptr(flags))
}

func setWindowLong(hwnd uintptr, index int, value uintptr) {
	procSetWindowLong.Call(hwnd, uintptr(index), value)
}

// monitorOf returns the monitor a window is on.
func monitorOf(hwnd uintptr) (rect, bool) {
	mi, ok := monitorInfoOf(hwnd)
	return mi.rcMonitor, ok
}

// workAreaOf returns the area of the window's monitor without the taskbar.
func workAreaOf(hwnd uintptr) (rect, bool) {
	mi, ok := monitorInfoOf(hwnd)
	return mi.rcWork, ok
}

func monitorInfoOf(hwnd uintptr) (monitorInfo, bool) {
	m, _, _ := procMonitorFromWin.Call(hwnd, 2 /* MONITOR_DEFAULTTONEAREST */)
	if m == 0 {
		return monitorInfo{}, false
	}
	mi := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	ok, _, _ := procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi, ok != 0
}

type player struct {
	empty bool

	dec  *decoder.Decoder
	info decoder.Info
	img  *image.RGBA
	bm   *ui.Bitmap
	ad   *decoder.Audio
	ap   *audio.Player
	win  *mygo.Window
	subs *subtitles

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
	bar      float32
	idle     time.Time
	dragging bool
	lastTap  time.Time
	keyHeld  bool
	keyLast  time.Time

	lastX float32
	lastY float32
	lastW float32
	lastH float32
	ticks int

	// window
	full       bool
	saved      rect
	savedStyle uintptr
	dragWin    bool
	dragMoved  bool
	dragFrom   point
	winFrom    rect

	// Esc asks twice before quitting.
	escAt time.Time

	// fileDrag is true while files dragged from another app hover.
	fileDrag bool

	// openPath is a file a drop or a second instance asked for.
	mu       sync.Mutex
	openPath string

	// The picture is drawn on demand, not on a clock: in is the input of
	// the frame being drawn, ctx the one Invalidate asks for another.
	in        *input
	ctx       *ui.Context
	fadeTimer *time.Timer
	eofAt     time.Time
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

func (p *player) startWindowDrag() {
	if p.win == nil || p.full {
		return
	}
	c, ok := cursorPos()
	if !ok {
		return
	}
	r, ok := windowRect(p.win.NativeHandle())
	if !ok {
		return
	}
	p.dragWin, p.dragMoved, p.dragFrom, p.winFrom = true, false, c, r
}

func (p *player) dragIfHeld() {
	if !p.dragWin {
		return
	}
	if !p.in.PointerDown(0) {
		p.dragWin = false
		return
	}
	c, ok := cursorPos()
	if !ok {
		return
	}
	dx, dy := c.X-p.dragFrom.X, c.Y-p.dragFrom.Y
	if !p.dragMoved && (dx > dragSlop || dx < -dragSlop || dy > dragSlop || dy < -dragSlop) {
		p.dragMoved = true
	}
	if p.dragMoved {
		setWindowPos(p.win.NativeHandle(), 0,
			p.winFrom.Left+dx, p.winFrom.Top+dy, 0, 0,
			swpNoSize|swpNoZOrder|swpNoActivate)
	}
}

// requestOpen asks for path to be played, if nothing plays yet.
func (p *player) requestOpen(path string) {
	p.mu.Lock()
	p.openPath = path
	p.mu.Unlock()
}

// load opens path and plays it. It is called on the UI thread, and only
// while nothing plays: a file dropped on a playing picture is ignored.
func (p *player) load(path string) {
	dec, info, err := decoder.Open(path)
	if err != nil || info.Width == 0 || info.Height == 0 {
		logf_("open ignored: %v", err)
		return
	}
	if _, err := dec.Next(); err != nil {
		dec.Close()
		logf_("open ignored: %v", err)
		return
	}
	ad, aerr := decoder.OpenAudio(path)

	if p.dec != nil {
		p.dec.Close()
	}
	if p.ap != nil {
		p.ap.Close()
	}
	if p.ad != nil {
		p.ad.Close()
	}
	p.dec, p.info, p.ad, p.ap = dec, info, nil, nil
	p.subs = loadSubtitles(path)
	if aerr == nil {
		p.ad = ad
		p.ap = audio.NewPlayer(ad)
		p.ap.Play()
	}
	p.img = &image.RGBA{
		Pix:    dec.Pixels(),
		Stride: info.Width * 4,
		Rect:   image.Rect(0, 0, info.Width, info.Height),
	}
	p.bm = ui.NewBitmap(p.img)
	p.bm.Update(p.img)
	p.empty = false
	p.base, p.started = 0, time.Now()
	p.paused, p.eof, p.pending = false, false, nil
	p.bar, p.idle = 1, time.Now()
	if p.full {
		p.toggleFullscreen()
	}
	if p.win != nil {
		w, h := fitToScreen(info.Width, info.Height)
		p.win.SetContentSize(w, h)
		p.keepOnScreen()
	}
	logf_("loaded %s %dx%d", path, info.Width, info.Height)
}

// mute silences the sound while seeks churn, and restores the volume after.
func (p *player) mute(on bool) {
	if p.ap == nil {
		return
	}
	if on {
		p.ap.SetVolume(0)
	} else {
		p.ap.SetVolume(p.volume)
	}
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

// seekTo moves the picture and the sound to t, decoding the frames between
// the keyframe and t without showing them.
func (p *player) seekTo(t time.Duration) {
	if t < 0 {
		t = 0
	}
	if d := p.duration(); t > d {
		t = d
	}
	start := time.Now()
	f, err := p.dec.SeekTo(t)
	p.eof = false
	p.pending = nil
	p.base = t
	p.started = time.Now()
	landed := time.Duration(-1)
	if err == nil {
		if f != nil {
			landed = f.PTS
		}
		p.bm.Update(p.img)
		p.shown++
	}
	if p.ap != nil {
		p.ap.Seek(t)
	}
	logf_("seek %v -> %v in %v", t.Round(time.Millisecond), landed, time.Since(start).Round(time.Millisecond))
}

// pump shows decoded frames while they are due at pos.
func (p *player) pump(pos time.Duration, max int) {
	for i := 0; i < max && !p.eof; i++ {
		if p.pending == nil {
			f, err := p.dec.Next()
			if err != nil {
				p.eof = true
				p.eofAt = time.Now().Add(1200 * time.Millisecond)
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

// setDwm tells the window manager how to draw the window's frame: Windows
// 11 rounds frameless windows and paints a one pixel border, which shows at
// the edge of a fullscreen picture.
func setDwm(hwnd uintptr, corners int32, border uint32) {
	procDwmSetAttribute.Call(hwnd, dwmwaCornerPreference, uintptr(unsafe.Pointer(&corners)), 4)
	procDwmSetAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&border)), 4)
}

func (p *player) toggleFullscreen() {
	if p.win == nil {
		return
	}
	hwnd := p.win.NativeHandle()
	if p.full {
		setWindowLong(hwnd, gwlStyle, p.savedStyle)
		w := p.saved.Right - p.saved.Left
		h := p.saved.Bottom - p.saved.Top
		setWindowPos(hwnd, hwndNotTop, p.saved.Left, p.saved.Top, w, h, swpFrameChanged)
		setDwm(hwnd, dwmcpDefault, dwmColorDefault)
		p.full = false
		return
	}
	m, ok := monitorOf(hwnd)
	if !ok {
		return
	}
	p.saved, _ = windowRect(hwnd)
	idx := int32(gwlStyle)
	style, _, _ := procGetWindowLong.Call(hwnd, uintptr(idx))
	p.savedStyle = uintptr(style)
	setWindowLong(hwnd, gwlStyle, uintptr(style&^(wsCaption|wsThickFrame)))
	setWindowPos(hwnd, hwndTop, m.Left, m.Top, m.Right-m.Left, m.Bottom-m.Top, swpShowWindow|swpFrameChanged)
	setDwm(hwnd, dwmcpDoNotRound, dwmColorNone)
	p.full = true
}

func (p *player) update() {
	now := time.Now()
	x, y, over := p.in.Pointer()
	w, h := p.in.Size()

	if p.in.Pressed(ui.KeyEscape) {
		switch {
		case p.empty:
			mygo.App.Quit() // nothing to lose: one press quits the empty window
		case p.full:
			p.toggleFullscreen()
		case now.Sub(p.escAt) < escConfirm:
			mygo.App.Quit()
		default:
			p.escAt = now
		}
		return
	}

	p.mu.Lock()
	pending := p.openPath
	p.openPath = ""
	p.mu.Unlock()
	if pending != "" {
		p.load(pending)
	}
	if p.eof && !p.eofAt.IsZero() && now.After(p.eofAt) {
		p.reset()
	}

	if p.empty {
		// The empty window only drags; nothing else answers.
		if p.in.PointerPressed(0) {
			p.startWindowDrag()
		}
		p.dragIfHeld()
		return
	}
	if p.in.Pressed(ui.KeySpace) {
		p.setPaused(!p.paused)
	}

	// Arrow keys: a tap steps two seconds; holding repeats the step as
	// fast as the picture can follow.
	step := func(k ui.Key, dir time.Duration) {
		if p.in.Pressed(k) {
			p.mute(true)
			p.seekTo(p.position() + dir)
			p.keyHeld, p.keyLast = true, now
			p.idle = now
		} else if p.in.Down(k) && p.keyHeld && now.Sub(p.keyLast) > 200*time.Millisecond {
			p.seekTo(p.position() + dir)
			p.keyLast = now
			p.idle = now
		} else if p.in.Released(k) && p.keyHeld {
			p.keyHeld = false
			p.mute(false)
		}
	}
	step(ui.KeyLeft, -2*time.Second)
	step(ui.KeyRight, 2*time.Second)

	// Volume: the wheel, with nothing on screen to show it.
	// The wheel arrives as about ±100 per notch: scrolling down is
	// positive here, and lowers the volume.
	if _, dy := p.in.Wheel(); dy != 0 && p.ap != nil {
		p.volume = min(max(p.volume-float64(dy)/100*0.05, 0), 1)
		p.ap.SetVolume(p.volume)
		p.idle = now
		logf_("volume %.0f%%", p.volume*100)
	}

	inBar := over && y >= h-barHeight
	if p.in.PointerPressed(0) {
		if inBar {
			p.dragging = true
			p.mute(true)
			p.idle = now
		} else if now.Sub(p.lastTap) < 400*time.Millisecond {
			p.toggleFullscreen()
		} else {
			p.idle = now
			p.startWindowDrag()
		}
		p.lastTap = now
	}
	if p.dragging {
		if p.in.PointerDown(0) {
			p.seekTo(time.Duration(float64(x) / float64(w) * float64(p.duration())))
			p.idle = now
		} else {
			p.dragging = false
			p.mute(false)
		}
	}
	p.dragIfHeld()

	// The bar and the clock show when the mouse moves, and fade after two
	// seconds of stillness.
	if over && (x != p.lastX || y != p.lastY) {
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

	if p.paused || p.eof || !p.ready || p.fileDrag {
		return
	}
	p.pump(p.position(), 4)
}

func (p *player) draw(pt *ui.Painter, r ui.Rect) {
	if !p.ready {
		p.ready = true
		p.started = time.Now()
		p.idle = time.Now()
		if ws := mygo.Windows(); len(ws) > 0 {
			p.win = ws[0]
		}
		logf_("first frame after %v", time.Since(t0).Round(time.Millisecond))
	}
	pt.Fill(r, ui.RGBA(0, 0, 0, 1), 0)

	if p.empty {
		p.drawCentered(pt, r, "把视频拖到这里，或双击视频文件播放", 16, ui.RGBA(255, 255, 255, 0.55))
		return
	}

	pt.Image(p.bm, r, ui.Contain)

	if line := p.subs.at(p.position()); line != "" {
		p.drawSubtitle(pt, r, line)
	}

	if time.Since(p.escAt) < escConfirm {
		p.drawCentered(pt, r, "再按一次 Esc 退出", 15, ui.RGBA(255, 255, 255, 0.7))
	}

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
	x := r.X + (r.W-tw)/2
	pt.Text(x, bar.Y-th-8, s, 13, col)

	// The volume follows the clock, and only shows with it.
	vol := fmt.Sprintf("%.0f%%", p.volume*100)
	vw, _ := pt.MeasureText(0, ui.Span{Text: vol, Size: 13})
	pt.Text(x+tw+14, bar.Y-th-8, vol, 13, ui.RGBA(255, 255, 255, 0.6*a))
	_ = vw
}

// view builds the window's picture: one element that fills it, takes the
// input, and paints on demand instead of on a clock of its own.
func (p *player) view(c *ui.Context) {
	p.ctx = c
	e := ui.Box(c).Fill().Focusable().AutoFocus()
	e.HandleInput(p.in.handle)
	e.Draw(p.paint)
	// The picture is the drop target too: it reports the drag so the system
	// drag image can move while nothing else runs, and it takes the files.
	p.fileDrag = e.FileDragOver()
	if files := e.DroppedFiles(); len(files) > 0 {
		p.requestOpen(files[0])
	}
}

// paint runs one frame: it moves the picture on, draws it, and asks for
// another frame only while something moves. A still picture costs nothing,
// which keeps the window free while files are dragged over it.
func (p *player) paint(pt *ui.Painter, r ui.Rect) {
	p.in.w, p.in.h = r.W, r.H
	p.update()
	p.in.step()
	p.draw(pt, r)
	if p.animating() {
		pt.AnimationFrame()
	} else {
		p.armFade()
	}
}

// animating reports whether the next frame has something new to show.
func (p *player) animating() bool {
	if p.fileDrag {
		return false // let the drag image move about freely
	}
	if p.dragging || p.dragWin {
		return true
	}
	if p.bar > 0.001 && p.bar < 0.999 {
		return true
	}
	if time.Since(p.escAt) < escConfirm {
		return true
	}
	if p.eof && !p.eofAt.IsZero() && time.Now().Before(p.eofAt) {
		return true
	}
	return !p.empty && !p.paused && !p.eof
}

// armFade wakes the window when the bar's two seconds of stillness are up:
// nothing else would draw the frame that starts fading it.
func (p *player) armFade() {
	if p.ctx == nil || p.bar <= 0 {
		return
	}
	p.mu.Lock()
	if p.fadeTimer != nil {
		p.mu.Unlock()
		return
	}
	d := time.Until(p.idle.Add(2 * time.Second))
	if d < 0 {
		d = 0
	}
	p.fadeTimer = time.AfterFunc(d, func() {
		p.mu.Lock()
		p.fadeTimer = nil
		p.mu.Unlock()
		p.ctx.Invalidate()
	})
	p.mu.Unlock()
}

// reset goes back to the window with no file in it, as when it starts.
func (p *player) reset() {
	if p.dec != nil {
		p.dec.Close()
		p.dec = nil
	}
	if p.ap != nil {
		p.ap.Close()
		p.ap = nil
	}
	if p.ad != nil {
		p.ad.Close()
		p.ad = nil
	}
	p.bm, p.img, p.pending, p.subs = nil, nil, nil, nil
	p.empty, p.eof, p.paused = true, false, false
	p.eofAt = time.Time{}
	p.bar, p.shown, p.dropped = 0, 0, 0
	p.base, p.started = 0, time.Now()
	if p.full {
		p.toggleFullscreen()
	}
	if p.win != nil {
		p.win.SetContentSize(emptyW, emptyH)
		p.keepOnScreen()
	}
	logf_("reset to empty")
}

// keepOnScreen leaves the window where it is, and only nudges it back if
// resizing pushed part of it off the monitor's work area.
func (p *player) keepOnScreen() {
	if p.win == nil {
		return
	}
	m, ok := workAreaOf(p.win.NativeHandle())
	if !ok {
		return
	}
	r, ok := windowRect(p.win.NativeHandle())
	if !ok {
		return
	}
	x, y := r.Left, r.Top
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if x+w > m.Right {
		x = m.Right - w
	}
	if y+h > m.Bottom {
		y = m.Bottom - h
	}
	if x < m.Left {
		x = m.Left
	}
	if y < m.Top {
		y = m.Top
	}
	if x == r.Left && y == r.Top {
		return
	}
	setWindowPos(p.win.NativeHandle(), 0, x, y, 0, 0,
		swpNoSize|swpNoZOrder|swpNoActivate)
}

// drawSubtitle paints the line centered above the bar, on a dim band so it
// reads over a bright picture too.
func (p *player) drawSubtitle(pt *ui.Painter, r ui.Rect, line string) {
	const size = 24
	maxW := r.W * 0.8
	span := ui.Span{Text: line, Size: size, Color: ui.RGBA(255, 255, 255, 0.97)}
	tw, th := pt.MeasureText(maxW, span)
	// Clear of the bar and the clock above it, whether they show or not.
	const bottom = barHeight + 44
	x := r.X + (r.W-tw)/2
	y := r.Y + r.H - bottom - th
	const pad = 12
	pt.Fill(ui.Rect{X: x - pad, Y: y - 8, W: tw + 2*pad, H: th + 16}, ui.RGBA(0, 0, 0, 0.42), 6)
	pt.RichText(x, y, maxW, span)
}

func (p *player) drawCentered(pt *ui.Painter, r ui.Rect, s string, size float32, c ui.Color) {
	tw, th := pt.MeasureText(0, ui.Span{Text: s, Size: size, Color: c})
	pt.Text(r.X+(r.W-tw)/2, r.Y+(r.H-th)/2, s, size, c)
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
