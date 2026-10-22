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
	VideoBitRate  int64 // average encoded bits/s; zero when unavailable
	AudioBitRate  int64
	HasAudio      bool

	// HW names the device decoding this file, empty when the pictures are
	// decoded on the CPU. It changes to empty if playback had to fall back.
	HW string
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

// ErrNoVideo is returned by Open for a file that has no picture, as an
// audio file: Cupbox plays pictures, and ignores those.
var ErrNoVideo = errors.New("decoder: no video stream")
