// Audioprobe decodes a file through Cupbox to raw 48 kHz stereo float32.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"cupbox/decoder"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: audioprobe input output.f32")
	}
	d, err := decoder.OpenAudio(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer d.Close()
	f, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	buf := make([]float32, 4096)
	bytes := make([]byte, len(buf)*4)
	var total int64
	var slowest time.Duration
	start := time.Now()
	for {
		t := time.Now()
		n, err := d.Read(buf)
		slowest = max(slowest, time.Since(t))
		for i, v := range buf[:n] {
			binary.LittleEndian.PutUint32(bytes[i*4:], math.Float32bits(v))
		}
		if _, e := f.Write(bytes[:n*4]); e != nil {
			panic(e)
		}
		total += int64(n / 2)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
	}
	fmt.Printf("frames=%d duration=%.6fs decode=%v slowest_read=%v\n", total, float64(total)/48000, time.Since(start), slowest)
}
