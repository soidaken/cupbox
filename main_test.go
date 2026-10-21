package main

import (
	"testing"
	"time"
)

func TestOverlayTarget(t *testing.T) {
	cases := []struct {
		name     string
		inside   bool
		overUI   bool
		dragging bool
		idle     time.Duration
		want     float32
	}{
		{"pointer over the bar stays, however long", true, true, false, time.Hour, 1},
		{"just moved, inside the window", true, false, false, 0, 1},
		{"just short of the delay", true, false, false, hideAfter - time.Millisecond, 1},
		{"rested inside the window", true, false, false, hideAfter + time.Millisecond, 0},
		{"left immediately after moving", false, false, false, 0, 0},
		{"old hover coordinates outside", false, true, false, 0, 0},
		{"dragging outside the window", false, false, true, time.Hour, 1},
	}
	for _, c := range cases {
		if got := overlayTarget(c.inside, c.overUI, c.dragging, c.idle); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOverlayFade(t *testing.T) {
	for _, fps := range []int{24, 30, 60, 144} {
		dt := time.Second / time.Duration(fps)
		alpha := float32(0)
		for elapsed := time.Duration(0); elapsed < 150*time.Millisecond; elapsed += dt {
			alpha = fadeOverlay(alpha, 1, dt)
		}
		if alpha != 1 {
			t.Fatalf("%d fps: fade-in did not settle: %v", fps, alpha)
		}
		for elapsed := time.Duration(0); elapsed < 250*time.Millisecond; elapsed += dt {
			alpha = fadeOverlay(alpha, 0, dt)
		}
		if alpha != 0 {
			t.Fatalf("%d fps: fade-out did not settle: %v", fps, alpha)
		}
	}
}

func TestSeekTrack(t *testing.T) {
	for _, c := range []struct{ x, want float64 }{{0, 0}, {20, 0}, {500, .5}, {980, 1}, {1000, 1}} {
		if got := seekFraction(float32(c.x), 1000); got != c.want {
			t.Errorf("x=%v: got %v, want %v", c.x, got, c.want)
		}
	}
}

func TestChooseClock(t *testing.T) {
	cases := []struct {
		name     string
		audioPos time.Duration
		late     bool
		since    time.Duration
		want     clockChoice
	}{
		{"the sound plays", 100 * time.Millisecond, false, 0, clockSound},
		{"the sound has not started yet", 0, false, 100 * time.Millisecond, clockWait},
		{"still inside the grace", 0, false, audioStartGrace - time.Millisecond, clockWait},
		{"the grace is over", 0, false, audioStartGrace, clockPicture},
		{"the sound is late, whatever it reports", 0, true, time.Second, clockPicture},
		{"the sound joined but has to be skipped first", 100 * time.Millisecond, true, time.Second, clockPicture},
	}
	for _, c := range cases {
		if got := chooseClock(c.audioPos, c.late, c.since); got != c.want {
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
