// Package decoder turns a media file into RGBA frames and PCM samples.
//
// It knows nothing about windows, rendering or playback: it opens a
// container, reports the video's size and duration, and hands out frames
// one at a time with their presentation time.
package decoder

import (
	"errors"
	"time"
)

// Info describes the video of a file.
type Info struct {
	Width, Height int
	Duration      time.Duration
	FPS           float64
}

// Frame is one decoded picture as premultiplied-compatible RGBA bytes:
// len(RGBA) == Width*Height*4, opaque, top row first. The bytes belong to
// the Decoder and stay valid until the next call to Next, which reuses
// them.
type Frame struct {
	PTS  time.Duration
	RGBA []byte
}

// ErrEOF is returned by Next after the last frame.
var ErrEOF = errors.New("decoder: end of file")
