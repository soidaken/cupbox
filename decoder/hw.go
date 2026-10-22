package decoder

/*
#include <stdint.h>
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Note, when set, is told what the device did: why a stream was not given
// to it, and when playback fell back to the CPU.
var Note = func(string) {}

// hwMode is what CUPBOX_HW asks for: on and off override the automatic
// decision, for comparing the two paths on one machine.
type hwMode int

const (
	hwAuto hwMode = iota
	hwOff
	hwOn
)

func hwFromEnv() hwMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CUPBOX_HW"))) {
	case "off", "no", "false", "0":
		return hwOff
	case "on", "yes", "true", "1":
		return hwOn
	}
	return hwAuto
}

// hwStream identifies a stream for the memory of what the device refused:
// the same codec, profile and size decodes the same way on this machine.
type hwStream struct {
	codec, profile, width, height int
}

var (
	hwMu  sync.Mutex
	hwBad = map[hwStream]bool{}
)

// hwPreferred is whether playback uses the device by itself. Measured with
// our own decoder on an RTX 3060, copying every picture back to the memory
// the renderer reads from costs more than decoding on the CPU: 1080p 1.0 ->
// 8.6 ms a frame, 4K 4.1 -> 39 ms. Until the pictures stay on the device
// (docs/DESIGN.md), the automatic path decodes on the CPU; CUPBOX_HW=on
// still asks for the device, and a driver that cannot decode a file falls
// back to the CPU either way.
const hwPreferred = false

// hwMightBeUsed reports whether asking for the device is worth the startup
// cost at all: the automatic path does not need one while it decodes on the
// CPU, and creating a device takes about 150ms.
func hwMightBeUsed() bool {
	mode := hwFromEnv()
	return mode == hwOn || (mode == hwAuto && hwPreferred)
}

// hwUse reports whether a stream should be given to the device. The build
// and the machine are already known to have one; this is the rest of the
// policy: ask for it only when it is preferred, fits what the device
// decodes, and did not fail here before.
func hwUse(mode hwMode, preferred bool, width, height, maxWidth, maxHeight int, refused bool) bool {
	switch mode {
	case hwOff:
		return false
	case hwOn:
		return true
	}
	if !preferred || refused {
		return false
	}
	if maxWidth > 0 && width > maxWidth {
		return false
	}
	if maxHeight > 0 && height > maxHeight {
		return false
	}
	return true
}

// firstFrameBudget is how long the first picture may take on the device
// before the stream is decoded on the CPU instead.
const firstFrameBudget = 1500 * time.Millisecond

// hwTransferFails is how many frames the device may fail to hand over
// before the stream continues on the CPU.
const hwTransferFails = 3

//export goHWAllowed
func goHWAllowed(codec, profile, width, height, maxWidth, maxHeight C.int) C.int {
	if width <= 0 || height <= 0 {
		return 0
	}
	key := hwStream{int(codec), int(profile), int(width), int(height)}
	hwMu.Lock()
	refused := hwBad[key]
	hwMu.Unlock()
	mode := hwFromEnv()
	if mode == hwAuto && refused {
		Note(fmt.Sprintf("%dx%d failed on the device before; decoding on the CPU", int(width), int(height)))
		return 0
	}
	if hwUse(mode, hwPreferred, int(width), int(height), int(maxWidth), int(maxHeight), refused) {
		return 1
	}
	if mode == hwAuto {
		hwOnce.Do(func() {
			Note("the device could decode this, but copying its pictures back is slower than the CPU; CUPBOX_HW=on tries it")
		})
	}
	return 0
}

// hwOnce says the note above once per run.
var hwOnce sync.Once

//export goHWFailed
func goHWFailed(codec, profile, width, height C.int) {
	key := hwStream{int(codec), int(profile), int(width), int(height)}
	hwMu.Lock()
	hwBad[key] = true
	hwMu.Unlock()
}
