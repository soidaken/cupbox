// Cupbox: a video player with no chrome, no buttons, and nothing but the
// picture.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
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
	barHeight = 32
	emptyW    = 960
	emptyH    = 640
	dragSlop  = 4                      // pixels a press may wander before it moves the window
	moveSlop  = 5                      // pixels the pointer must travel to count as activity
	hideAfter = 100 * time.Millisecond // how long the overlay stays after the pointer rests
)

// iconPNG is the window icon: the same picture the executable carries, so
// the taskbar shows it in development too.
//
//go:embed assets/icon-256.png
var iconPNG []byte

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
		if errors.Is(err, decoder.ErrNoVideo) || (err == nil && (info.Width == 0 || info.Height == 0)) {
			// An audio file, or anything else without a picture: Cupbox
			// plays pictures only, and says nothing.
			os.Exit(0)
		}
		if err != nil {
			fail(err.Error())
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
		p.subStyle = parseASSHeader(dec.SubtitleHeader())
		p.subW, p.subH = dec.SubtitleSize()
		p.embedded = append(p.embedded, dec.Subtitles()...)

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

	if bmp, err := ui.DecodeBitmap(iconPNG); err == nil {
		p.iconBmp = bmp
	}

	mygo.App.WhenReady(func() {
		w, h := winW, winH
		if p.dec != nil {
			w, h = fitToScreen(p.info.Width, p.info.Height)
		}
		x, y := cascadePosition(w, h)
		logf_("place %dx%d at %d,%d", w, h, x, y)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "Cupbox",
			Width:          w,
			Height:         h,
			X:              x,
			Y:              y,
			UseContentSize: true,
			Frameless:      true,
			DisableResize:  true,
			Content:        ui.View(p.view),
		})
		if err := win.SetIcon(iconPNG); err != nil {
			logf_("window icon: %v", err)
		}
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
	logf_("fatal: %s", msg)
	alert(msg)
	os.Exit(1)
}

// alert shows a one-line message in a native box: a windowed program has
// nowhere else to say what went wrong.
func alert(msg string) {
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("Cupbox")
	const mbIconError, mbSetForeground, mbTopMost = 0x10, 0x10000, 0x40000
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)),
		mbIconError|mbSetForeground|mbTopMost)
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
	user32                   = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procMonitorFromWin       = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW      = user32.NewProc("GetMonitorInfoW")
	procGetWindowLong        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLong        = user32.NewProc("SetWindowLongPtrW")
	procForeground           = user32.NewProc("GetForegroundWindow")
	procWindowThread         = user32.NewProc("GetWindowThreadProcessId")
	procAttachInput          = user32.NewProc("AttachThreadInput")
	procBringToTop           = user32.NewProc("BringWindowToTop")
	procSetForeground        = user32.NewProc("SetForegroundWindow")
	procSetFocus             = user32.NewProc("SetFocus")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procCurrentThread        = kernel32.NewProc("GetCurrentThreadId")
	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procQueryImageName       = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	dwmapi                   = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetAttribute      = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	swpNoMove       = 0x0002
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
	subs *subtitles // a sidecar .srt next to the video

	embedded   []decoder.SubtitleEvent // the file's own subtitle track
	subStyle   assStyle
	subW, subH int        // the canvas picture subtitles are laid out on
	subBmp     *ui.Bitmap // the picture subtitle being shown
	subBmpAt   string

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
	bar           float32
	idle          time.Time
	dragging      bool
	overBar       bool
	overUI        bool
	pointerInside bool
	wasOver       bool
	anchorX       float32
	anchorY       float32
	volAt         time.Time
	iconBmp       *ui.Bitmap
	lastTap       time.Time
	keyHeld       bool
	keyLast       time.Time

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

// cascadePosition returns where a window of w×h goes: 30px right and down
// from the last Cupbox window on screen, so that windows do not hide each
// other, centred when it is the only one.
func cascadePosition(w, h int) (int, int) {
	work, ok := primaryWorkArea()
	if !ok {
		return 0, 0
	}
	ww, hh := int32(w), int32(h)
	center := func() (int32, int32) {
		return work.Left + (work.Right-work.Left-ww)/2, work.Top + (work.Bottom-work.Top-hh)/2
	}
	last, found := lastCupboxWindow()
	var x, y int32
	if found {
		x, y = last.Left+30, last.Top+30
	} else {
		x, y = center()
	}
	if x+ww > work.Right || y+hh > work.Bottom {
		x, y = center()
	}
	if x < work.Left {
		x = work.Left
	}
	if y < work.Top {
		y = work.Top
	}
	return int(x), int(y)
}

// primaryWorkArea returns the main display's area without the taskbar, in
// screen pixels.
func primaryWorkArea() (rect, bool) {
	var r rect
	ok, _, _ := procSystemParametersInfo.Call(0x0030 /* SPI_GETWORKAREA */, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r, ok != 0
}

// lastCupboxWindow returns the lowest-right window of another Cupbox
// process, the one a new window steps away from.
func lastCupboxWindow() (rect, bool) {
	self := uint32(os.Getpid())
	var best rect
	found := false
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var pid uint32
		procWindowThread.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || pid == self || !cupboxProcess(pid) {
			return 1
		}
		// EnumWindows walks the z-order from the front: the first Cupbox
		// window is the one in front, and the one to step away from.
		if r, ok := windowRect(hwnd); ok && r.Right > r.Left && r.Bottom > r.Top {
			best, found = r, true
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return best, found
}

// cupboxProcess reports whether pid runs cupbox.exe.
func cupboxProcess(pid uint32) bool {
	h, _, _ := procOpenProcess.Call(0x1000 /* PROCESS_QUERY_LIMITED_INFORMATION */, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	ok, _, _ := procQueryImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || n == 0 {
		return false
	}
	name := syscall.UTF16ToString(buf[:n])
	return strings.EqualFold(filepath.Base(name), "cupbox.exe")
}

// raise brings the window to the front and gives it the keyboard, as
// starting a picture should. Windows lets a program started by another one
// take the foreground only while attached to the foreground thread.
func (p *player) raise() {
	if p.win == nil {
		return
	}
	hwnd := p.win.NativeHandle()
	// Top of the z-order, without staying above other windows.
	setWindowPos(hwnd, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	setWindowPos(hwnd, hwndNotTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)

	fg, _, _ := procForeground.Call()
	ft, _, _ := procWindowThread.Call(fg, 0)
	mt, _, _ := procCurrentThread.Call()
	attached := false
	if ft != 0 && ft != mt {
		procAttachInput.Call(mt, ft, 1)
		attached = true
	}
	procBringToTop.Call(hwnd)
	procSetForeground.Call(hwnd)
	procSetFocus.Call(hwnd)
	if attached {
		procAttachInput.Call(mt, ft, 0)
	}
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

// requestOpen asks for path to be played. It is called from the drop
// target and from a second instance, off the UI thread, so it wakes the
// window: an idle one (empty, or paused) draws no frames of its own, and
// without a frame the file would never be opened.
func (p *player) requestOpen(path string) {
	p.mu.Lock()
	p.openPath = path
	ctx := p.ctx
	p.mu.Unlock()
	if ctx != nil {
		ctx.Invalidate()
	}
}

// load opens path and plays it. It is called on the UI thread, and only
// while nothing plays: a file dropped on a playing picture is ignored.
func (p *player) load(path string) {
	dec, info, err := decoder.Open(path)
	if errors.Is(err, decoder.ErrNoVideo) || (err == nil && (info.Width == 0 || info.Height == 0)) {
		return // audio only: dropped files with no picture are ignored
	}
	if err == nil {
		if _, derr := dec.Next(); derr != nil {
			err = derr
		}
	}
	if err != nil {
		if dec != nil {
			dec.Close()
		}
		logf_("open ignored: %v", err)
		alert("打不开这个文件：" + err.Error())
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
	p.subStyle = parseASSHeader(dec.SubtitleHeader())
	p.subW, p.subH = dec.SubtitleSize()
	p.embedded = append(p.embedded[:0], dec.Subtitles()...)
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
	p.raise()
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
	p.embedded = append(p.embedded[:0], p.dec.Subtitles()...)
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
			p.embedded = append(p.embedded, p.dec.Subtitles()...)
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
	x, y, _ := p.in.Pointer()
	w, h := p.in.Size()
	// Where the pointer really is: the last move event stays where it was
	// after the pointer leaves the window, and a pointer that stops over the
	// overlay never sends another one.
	over := false
	if p.win != nil {
		if c, ok := cursorPos(); ok {
			if r, ok := windowRect(p.win.NativeHandle()); ok {
				over = c.X >= r.Left && c.X < r.Right && c.Y >= r.Top && c.Y < r.Bottom
			}
		}
	}

	if p.in.Pressed(ui.KeyEscape) {
		if p.full {
			p.toggleFullscreen() // a fullscreen picture goes back to its window
		} else {
			mygo.App.Quit()
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
		p.idle = now
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
		p.volAt = now
		logf_("volume %.0f%%", p.volume*100)
	}

	inBar := over && y >= h-barHeight
	p.overBar = inBar
	// The overlay stays while the pointer rests over it: the bar, the clock
	// above it, and the scrim they sit on.
	p.overUI = over && y >= h-(barHeight+40)
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

	// Activity is entering the window, or moving more than a few pixels:
	// a hand resting on a mouse still reports a pixel of movement now and
	// then, which must not keep the overlay alive.
	if over {
		moved := movedFar(x, y, p.anchorX, p.anchorY)
		if !p.wasOver || moved {
			p.idle = now
		}
		if !p.wasOver || moved || p.anchorX == 0 && p.anchorY == 0 {
			p.anchorX, p.anchorY = x, y
		}
	}
	p.wasOver = over
	p.lastX, p.lastY, p.lastW, p.lastH = x, y, w, h
	target := overlayTarget(p.overUI, p.dragging, now.Sub(p.idle))
	p.bar += (target - p.bar) * 0.25
	if p.bar < 0.01 {
		p.bar = 0
	}

	if p.paused || p.eof || !p.ready || p.fileDrag {
		return
	}
	p.pump(p.position(), 4)

	// Forget the subtitles that are behind us.
	pos := p.position()
	keep := p.embedded[:0]
	for _, ev := range p.embedded {
		if ev.End > pos-2*time.Second {
			keep = append(keep, ev)
		}
	}
	p.embedded = keep
}

func (p *player) draw(pt *ui.Painter, r ui.Rect) {
	if !p.ready {
		p.ready = true
		p.started = time.Now()
		p.idle = time.Now()
		if ws := mygo.Windows(); len(ws) > 0 {
			p.win = ws[0]
		}
		if !p.empty {
			p.raise()
		}
		logf_("first frame after %v", time.Since(t0).Round(time.Millisecond))
	}
	pt.Fill(r, ui.RGBA(0, 0, 0, 1), 0)

	if p.empty {
		p.drawEmpty(pt, r)
		return
	}

	pt.Image(p.bm, r, ui.Contain)

	pos := p.position()
	if line := p.subs.at(pos); line != "" {
		p.drawSubtitle(pt, p.videoRect(r), line)
	} else {
		for _, ev := range p.embedded {
			if ev.Start <= pos+30*time.Millisecond && pos < ev.End {
				if ev.Bitmap != nil {
					p.drawBitmapSub(pt, r, ev)
				} else {
					p.drawEmbedded(pt, p.videoRect(r), ev)
				}
			}
		}
	}

	if p.bar <= 0 {
		return
	}
	a := p.bar
	hit := ui.Rect{X: r.X, Y: r.Y + r.H - barHeight, W: r.W, H: barHeight}

	// A scrim, not a slab: the picture stays visible under the bar, and the
	// text reads over a white frame.
	// The scrim stops below the subtitles, which keep a background of
	// their own: none.
	pt.FillGradient(ui.Rect{X: r.X, Y: r.Y + r.H - 72, W: r.W, H: 72},
		ui.LinearGradient{From: ui.RGBA(0, 0, 0, 0), To: ui.RGBA(0, 0, 0, 0.55*a), Angle: 180}, 0)

	const inset = 20
	th := float32(3)
	if p.overBar || p.dragging {
		th = 5
	}
	if p.dragging {
		th = 6
	}
	cy := hit.Y + hit.H - 20
	track := ui.Rect{X: hit.X + inset, Y: cy - th/2, W: hit.W - 2*inset, H: th}
	pt.Fill(track, ui.RGBA(255, 255, 255, 0.26*a), th/2)

	frac := float64(p.position()) / float64(p.duration())
	frac = min(max(frac, 0), 1)
	brand := ui.RGBA(255, 214, 10, 0.95*a)
	if frac > 0 {
		pt.Fill(ui.Rect{X: track.X, Y: track.Y, W: float32(frac) * track.W, H: th}, brand, th/2)
	}
	if p.overBar || p.dragging {
		x := track.X + float32(frac)*track.W
		pt.Fill(ui.Rect{X: x - 7, Y: cy - 7, W: 14, H: 14}, ui.RGBA(255, 214, 10, 0.16*a), 7)
		pt.Fill(ui.Rect{X: x - 4.5, Y: cy - 4.5, W: 9, H: 9}, brand, 4.5)
	}

	// The clock: elapsed is the lead, the whole is quiet behind it.
	cur := clock(p.position())
	tot := clock(p.duration())
	first := []ui.Span{
		{Text: cur, Size: 15, Weight: 600, Color: ui.RGBA(255, 255, 255, 0.92*a)},
		{Text: "  /  ", Size: 13, Color: ui.RGBA(255, 255, 255, 0.30*a)},
		{Text: tot, Size: 13, Color: ui.RGBA(255, 255, 255, 0.42*a)},
	}
	tw, thh := pt.MeasureText(0, first...)
	ty := hit.Y - thh - 10 + (1-a)*6
	pt.RichText(r.X+(r.W-tw)/2, ty, 0, first...)

	// The volume sits at the right edge, at the clock's baseline.
	flash := float32(time.Since(p.volAt)) / float32(700*time.Millisecond)
	flash = min(max(1-flash, 0), 1)
	vol := fmt.Sprintf("%d%%", int(p.volume*100+0.5))
	vspan := []ui.Span{{Text: vol, Size: 12, Color: ui.RGBA(255, 255, 255, (0.45+0.45*flash)*a)}}
	vw, vh := pt.MeasureText(0, vspan...)
	pt.RichText(r.X+r.W-inset-vw, ty+thh-vh, 0, vspan...)

	// Dragging shows the time the pointer is over.
	if p.dragging {
		label := clock(time.Duration(float64(p.lastX-inset-hit.X) / float64(track.W) * float64(p.duration())))
		span := []ui.Span{{Text: label, Size: 11, Color: ui.RGBA(255, 255, 255, 0.95*a)}}
		lw, lh := pt.MeasureText(0, span...)
		bw, bh := lw+18, lh+10
		bx := float32(p.lastX) - bw/2
		bx = min(max(bx, r.X+8), r.X+r.W-8-bw)
		by := hit.Y - 72
		pt.Fill(ui.Rect{X: bx, Y: by, W: bw, H: bh}, ui.RGBA(0, 0, 0, 0.62*a), 8)
		pt.RichText(bx+9, by+5, 0, span...)
	}
}

// overlayTarget says whether the overlay shows: over the UI, while dragging,
// or within hideAfter of the last activity.
func overlayTarget(overUI, dragging bool, idle time.Duration) float32 {
	if overUI || dragging || idle < hideAfter {
		return 1
	}
	return 0
}

// movedFar reports whether the pointer travelled far enough to count as
// activity: a hand resting on a mouse reports a pixel or two now and then.
func movedFar(x, y, ax, ay float32) bool {
	return math.Hypot(float64(x-ax), float64(y-ay)) > moveSlop
}

// drawEmpty paints the window with nothing in it: the mark, what to do, and
// what it plays.
func (p *player) drawEmpty(pt *ui.Painter, r ui.Rect) {
	if p.iconBmp != nil {
		const d = 88
		dst := ui.Rect{X: r.X + (r.W-d)/2, Y: r.Y + r.H/2 - d/2 - 58, W: d, H: d}
		pt.ImageRegion(p.iconBmp, ui.Rect{}, dst, ui.ImageOptions{Tint: ui.RGBA(255, 255, 255, 0.25)})
	}
	title := []ui.Span{{Text: "把视频拖到这里", Size: 17, Color: ui.RGBA(255, 255, 255, 0.62)}}
	tw, th := pt.MeasureText(0, title...)
	ty := r.Y + r.H/2 + 18
	pt.RichText(r.X+(r.W-tw)/2, ty, 0, title...)

	formats := []ui.Span{{Text: "MP4 · MKV · AVI · WebM · TS · WMV · MOV · FLV",
		Size: 12, Color: ui.RGBA(255, 255, 255, 0.30)}}
	fw, _ := pt.MeasureText(0, formats...)
	pt.RichText(r.X+(r.W-fw)/2, ty+th+10, 0, formats...)
}

func (p *player) view(c *ui.Context) {
	p.mu.Lock()
	p.ctx = c
	p.mu.Unlock()
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
	d := 150 * time.Millisecond // look again while the pointer may be over the overlay
	if !p.overUI {
		d = time.Until(p.idle.Add(hideAfter))
		if d < 0 {
			d = 0
		}
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

// drawSubtitle paints the cue centered above the bar: every line centered
// in x, the block centered as a whole. A shadow, not a background, keeps
// white text readable over a white picture.
func (p *player) drawSubtitle(pt *ui.Painter, r ui.Rect, text string) {
	const (
		size   = 24
		gap    = 2
		bottom = barHeight + 44
	)
	maxW := r.W * 0.8
	col := ui.RGBA(255, 255, 255, 0.97)
	shadow := ui.RGBA(0, 0, 0, 0.45)
	span := ui.Span{Text: text, Size: size, Color: col}

	lines := strings.Split(text, "\n")
	widths := make([]float32, len(lines))
	heights := make([]float32, len(lines))
	blockH := float32(0)
	for i, line := range lines {
		w, h := pt.MeasureText(0, ui.Span{Text: line, Size: size})
		if w > maxW {
			// Too long to center line by line: let the layout wrap it.
			tw, th := pt.MeasureText(maxW, span)
			x := r.X + (r.W-tw)/2
			y := r.Y + r.H - bottom - th
			sh := span
			sh.Color = shadow
			pt.RichText(x+1, y+1, maxW, sh)
			pt.RichText(x, y, maxW, span)
			return
		}
		widths[i], heights[i] = w, h
		blockH += h
	}
	blockH += gap * float32(len(lines)-1)

	y := r.Y + r.H - bottom - blockH
	for i, line := range lines {
		x := r.X + (r.W-widths[i])/2
		pt.Text(x+1, y+1, line, size, shadow)
		pt.Text(x, y, line, size, col)
		y += heights[i] + gap
	}
}

// videoRect is where the picture shows inside r: the window when they have
// the same shape, and letterboxed when not, as in fullscreen on another
// display's shape.
func (p *player) videoRect(r ui.Rect) ui.Rect {
	vw, vh := float32(p.info.Width), float32(p.info.Height)
	if vw <= 0 || vh <= 0 || r.W <= 0 || r.H <= 0 {
		return r
	}
	k := min(r.W/vw, r.H/vh)
	w, h := vw*k, vh*k
	return ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
}

// drawBitmapSub paints a picture subtitle, as Blu-ray and DVD carry, in the
// place of the video it was made for.
func (p *player) drawBitmapSub(pt *ui.Painter, r ui.Rect, ev decoder.SubtitleEvent) {
	key := fmt.Sprint(ev.Start, ev.End, ev.X, ev.Y, ev.W, ev.H, len(ev.Bitmap))
	if p.subBmp == nil || p.subBmpAt != key {
		img := &image.RGBA{Pix: ev.Bitmap, Stride: ev.W * 4, Rect: image.Rect(0, 0, ev.W, ev.H)}
		p.subBmp = ui.NewBitmap(img)
		p.subBmpAt = key
	}
	v := p.videoRect(r)
	cw, ch := p.subW, p.subH
	if cw <= 0 || ch <= 0 {
		cw, ch = p.info.Width, p.info.Height
	}
	sx, sy := v.W/float32(cw), v.H/float32(ch)
	dst := ui.Rect{
		X: v.X + float32(ev.X)*sx,
		Y: v.Y + float32(ev.Y)*sy,
		W: float32(ev.W) * sx,
		H: float32(ev.H) * sy,
	}
	pt.Image(p.subBmp, dst, ui.FillBox)
}

// drawEmbedded paints one subtitle of the file's own track, in the place
// and colour its style asks for.
func (p *player) drawEmbedded(pt *ui.Painter, r ui.Rect, ev decoder.SubtitleEvent) {
	lines, st := parseASSLines(assDialogueText(ev.Text), p.subStyle)
	scale := float64(1)
	if st.playResY > 0 {
		scale = float64(r.H) / st.playResY
	}
	size := float32(st.fontSize * scale)
	col := color.RGBA(st.color)
	tint := ui.RGBA(col.R, col.G, col.B, float32(col.A)/255)
	shadow := ui.RGBA(0, 0, 0, 0.45)

	row := (st.align - 1) / 3 // 0 bottom, 1 middle, 2 top
	colIdx := (st.align - 1) % 3
	const side = 40
	var total, gap float32 = 0, 2
	widths := make([]float32, len(lines))
	heights := make([]float32, len(lines))
	for i, line := range lines {
		w, h := pt.MeasureText(0, ui.Span{Text: line, Size: size})
		widths[i], heights[i] = w, h
		total += h
	}
	total += gap * float32(len(lines)-1)

	var y float32
	switch row {
	case 0:
		y = r.Y + r.H - (barHeight + 44) - total
	case 1:
		y = r.Y + (r.H-total)/2
	default:
		y = r.Y + side
	}
	for i, line := range lines {
		var x float32
		switch colIdx {
		case 0:
			x = r.X + side
		case 1:
			x = r.X + (r.W-widths[i])/2
		default:
			x = r.X + r.W - side - widths[i]
		}
		pt.Text(x+1, y+1, line, size, shadow)
		pt.Text(x, y, line, size, tint)
		y += heights[i] + gap
	}
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
