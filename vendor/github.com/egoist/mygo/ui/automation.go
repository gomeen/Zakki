//go:build !mygo_noinspector

package ui

import (
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// automate hands the window's content to plugins/automation, when the app
// links it and MYGO_AUTOMATION asks for it (surface.Automate). Production
// builds leave it out, as they do the inspector.
func (h *windowHost) automate() {
	if a := surface.Automate; a != nil {
		a(h.conn, automated{h})
	}
}

// automated is a window's content as plugins/automation reads it.
type automated struct{ h *windowHost }

func (a automated) AutomationTree() (*platform.AccessTree, map[uint64]any) {
	rt := a.h.rt
	keys := map[uint64]any{}
	var walk func(e *node)
	walk = func(e *node) {
		for ; e != nil; e = e.next {
			if e.key != nil {
				keys[e.id] = e.key
			}
			walk(e.first)
		}
	}
	walk(rt.c.root)
	return rt.windowAccess(rt.accessTree()), keys
}

// FramePending reports whether the content asked for a frame that has not
// come: in a window none of which shows, only one waiting to build
// (waitsOutOfSight), which Settle builds, as the frames it built wait for
// the window to show.
func (a automated) FramePending() bool {
	h := a.h
	switch {
	case h.detached:
		return false
	case h.occluded():
		return h.waitsOutOfSight()
	}
	return h.frameAsked
}

// Settle builds the frame that waits to build in a window none of which
// shows (waitsOutOfSight), whose surface may draw none before it shows: the
// view takes the input plugins/automation gave it, as a press, and the
// plugin reads and captures what the window shows now.
func (a automated) Settle() {
	if h := a.h; !h.detached && !h.rt.inFrame && h.waitsOutOfSight() {
		h.rt.runFrame()
	}
}
