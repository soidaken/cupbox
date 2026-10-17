package main

import "github.com/egoist/mygo/ui"

// input is what the pointer and keys did since the last frame: the state
// the player reads each frame, as the game plugin would hand it over, but
// without running a loop of its own.
type input struct {
	down     map[ui.Key]bool
	pressed  map[ui.Key]bool
	released map[ui.Key]bool

	x, y   float32
	inside bool

	buttons   [3]bool
	bPressed  [3]bool
	bReleased [3]bool

	wheelX, wheelY float32

	w, h float32
}

func newInput() *input {
	return &input{
		down:     map[ui.Key]bool{},
		pressed:  map[ui.Key]bool{},
		released: map[ui.Key]bool{},
	}
}

// handle takes an event from the window and reports whether the player
// used it.
func (in *input) handle(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown, ui.InputKeyUp:
		k := ev.PhysicalKey
		if k == ui.KeyUnknown {
			k = ev.Key
		}
		if ev.Kind == ui.InputKeyDown {
			if !ev.Repeat && !in.down[k] {
				in.pressed[k] = true
			}
			in.down[k] = true
		} else if in.down[k] {
			in.released[k] = true
			delete(in.down, k)
		}
		return true
	case ui.InputPointerDown, ui.InputPointerUp:
		in.x, in.y = ev.X, ev.Y
		in.inside = true
		if ev.Button >= 0 && ev.Button < 3 {
			down := ev.Kind == ui.InputPointerDown
			if down && !in.buttons[ev.Button] {
				in.bPressed[ev.Button] = true
			}
			if !down && in.buttons[ev.Button] {
				in.bReleased[ev.Button] = true
			}
			in.buttons[ev.Button] = down
		}
		return true
	case ui.InputPointerMove:
		in.x, in.y = ev.X, ev.Y
		in.inside = ev.X >= 0 && ev.Y >= 0 && ev.X < in.w && ev.Y < in.h
		return true
	case ui.InputScroll:
		in.wheelX += ev.DX
		in.wheelY += ev.DY
		return true
	}
	return false
}

// step forgets what was pressed, released or scrolled since the last frame.
func (in *input) step() {
	for k := range in.pressed {
		delete(in.pressed, k)
	}
	for k := range in.released {
		delete(in.released, k)
	}
	in.bPressed = [3]bool{}
	in.bReleased = [3]bool{}
	in.wheelX, in.wheelY = 0, 0
}

func (in *input) Down(k ui.Key) bool     { return in.down[k] }
func (in *input) Pressed(k ui.Key) bool  { return in.pressed[k] }
func (in *input) Released(k ui.Key) bool { return in.released[k] }

func (in *input) Pointer() (x, y float32, over bool) { return in.x, in.y, in.inside }
func (in *input) Size() (w, h float32)               { return in.w, in.h }
func (in *input) Wheel() (dx, dy float32)            { return in.wheelX, in.wheelY }

func (in *input) PointerDown(b int) bool     { return b >= 0 && b < 3 && in.buttons[b] }
func (in *input) PointerPressed(b int) bool  { return b >= 0 && b < 3 && in.bPressed[b] }
func (in *input) PointerReleased(b int) bool { return b >= 0 && b < 3 && in.bReleased[b] }
