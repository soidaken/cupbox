// Probe: can mygo push 1080p RGBA frames at display rate?
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/game"
	"github.com/egoist/mygo/ui"
)

const (
	frameW, frameH = 1920, 1080
	winW, winH     = 960, 540
	phaseDur       = 3 * time.Second
)

func cpuNow() time.Duration {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0
	}
	var c, e, k, u syscall.Filetime
	if err := syscall.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return 0
	}
	ft := func(f syscall.Filetime) time.Duration {
		n := int64(f.HighDateTime)<<32 | int64(f.LowDateTime)
		return time.Duration(n * 100)
	}
	return ft(k) + ft(u)
}

type probe struct {
	img *image.RGBA
	bm  *ui.Bitmap

	mode       int
	phaseStart time.Time
	cpuStart   time.Duration
	draws      int
	updates    int
	updateCost time.Duration
	firstLogged bool
}

func newProbe() *probe {
	img := image.NewRGBA(image.Rect(0, 0, frameW, frameH))
	for y := 0; y < frameH; y++ {
		for x := 0; x < frameW; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 255 / frameW), uint8(y * 255 / frameH), 40, 255})
		}
	}
	p := &probe{img: img, bm: ui.NewBitmap(img)}
	p.phaseStart = time.Now()
	p.cpuStart = cpuNow()
	return p
}

func (p *probe) Update(in *game.Input) {
	if p.mode == 1 {
		off := (p.updates * 64 * 1024) % len(p.img.Pix)
		for i := 0; i < 64*1024; i++ {
			p.img.Pix[(off+i)%len(p.img.Pix)] = byte(p.updates)
		}
		start := time.Now()
		p.bm.Update(p.img)
		p.updateCost += time.Since(start)
	}
	p.updates++
	p.maybePhase()
}

var t0 = time.Now()

func (p *probe) Draw(pt *ui.Painter, r ui.Rect, alpha float64) {
	if p.draws == 0 && !p.firstLogged {
		p.firstLogged = true
		fmt.Printf("first frame after %v\n", time.Since(t0).Round(time.Millisecond))
	}
	p.draws++
	pt.Image(p.bm, r, ui.FillBox)
}

func (p *probe) maybePhase() {
	if time.Since(p.phaseStart) < phaseDur {
		return
	}
	now := time.Now()
	cpu := cpuNow()
	el := now.Sub(p.phaseStart)
	fmt.Printf("phase=%d elapsed=%v draws=%d fps=%.1f cpu=%v cpu/frame=%.2fms update=%.2fms/frame\n",
		p.mode, el.Round(time.Millisecond), p.draws,
		float64(p.draws)/el.Seconds(),
		(cpu - p.cpuStart).Round(time.Millisecond),
		float64((cpu-p.cpuStart).Microseconds())/1000/float64(max(p.draws, 1)),
		float64(p.updateCost.Microseconds())/1000/float64(max(p.updates, 1)),
	)
	p.phaseStart = now
	p.cpuStart = cpu
	p.draws, p.updates, p.updateCost = 0, 0, 0
	p.mode++
	if p.mode > 1 {
		mygo.App.Quit()
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "cpu" {
		fmt.Println("cpu start", cpuNow())
		return
	}
	p := newProbe()
	fmt.Println("startup", time.Now().Format("15:04:05.000"))
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:          "probe",
			Width:          winW,
			Height:         winH,
			UseContentSize: true,
			Frameless:      true,
			DisableResize:  true,
			Content:        ui.View(func(c *ui.Context) { game.View(c, p) }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Println("run error:", err)
		os.Exit(1)
	}
	fmt.Println("done")
}
