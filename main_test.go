package main

import (
	"testing"
	"time"
)

func TestOverlayTarget(t *testing.T) {
	cases := []struct {
		name     string
		overUI   bool
		dragging bool
		idle     time.Duration
		want     float32
	}{
		{"pointer over the bar stays, however long", true, false, time.Hour, 1},
		{"just moved, inside the window", false, false, 0, 1},
		{"just short of the delay", false, false, hideAfter - time.Millisecond, 1},
		{"rested inside the window", false, false, hideAfter + time.Millisecond, 0},
		{"left the window", false, false, time.Hour, 0},
		{"dragging the bar", false, true, time.Hour, 1},
	}
	for _, c := range cases {
		if got := overlayTarget(c.overUI, c.dragging, c.idle); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMovedFar(t *testing.T) {
	cases := []struct {
		name         string
		x, y, ax, ay float32
		want         bool
	}{
		{"a pixel of jitter", 101, 100, 100, 100, false},
		{"four pixels", 104, 100, 100, 100, false},
		{"exactly the threshold", 105, 100, 100, 100, false},
		{"past the threshold", 106, 100, 100, 100, true},
		{"diagonal past it", 104, 104, 100, 100, true},
	}
	for _, c := range cases {
		if got := movedFar(c.x, c.y, c.ax, c.ay); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
