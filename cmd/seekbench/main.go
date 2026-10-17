package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"cupbox/decoder"
)

// Measures what an accurate seek costs: move to t, then decode forward
// until a frame at or after t is ready.
func main() {
	path := os.Args[1]
	targets := []float64{5, 12, 21.5, 27}
	if len(os.Args) > 2 {
		targets = targets[:0]
		for _, a := range os.Args[2:] {
			v, _ := strconv.ParseFloat(a, 64)
			targets = append(targets, v)
		}
	}
	d, info, err := decoder.Open(path)
	if err != nil {
		panic(err)
	}
	defer d.Close()
	fmt.Printf("%dx%d %v\n", info.Width, info.Height, info.Duration)
	for _, s := range targets {
		t := time.Duration(s * float64(time.Second))
		start := time.Now()
		if err := d.Seek(t); err != nil {
			panic(err)
		}
		seekT := time.Since(start)
		frames := 0
		first := time.Duration(-1)
		var got time.Duration
		for {
			f, err := d.Next()
			if err != nil {
				break
			}
			frames++
			if first < 0 {
				first = time.Since(start)
			}
			if f.PTS+20*time.Millisecond >= t {
				got = f.PTS
				break
			}
			if frames > 600 {
				break
			}
		}
		fmt.Printf("seek %.1fs: avseek=%v keyframe-to-target=%d frames, landed=%v, total=%v\n",
			s, seekT.Round(time.Millisecond), frames, got, time.Since(start).Round(time.Millisecond))
		_ = first
	}
}
