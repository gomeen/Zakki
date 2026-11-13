package ui

import "unsafe"

// The journal keeps what the view built of the fields that the frame
// writes after the build: a table's fitted widths, a toolbar's collapsed
// controls, the boxes, transforms and opacity transitions move, the spans
// of a paragraph merged from its children. An element kept to the next
// frame without its view running again goes back to what it was built
// with (rollback) before that frame lays it out and writes them anew.
// What it computes in fields of their own (the boxes layout gives, world,
// ink) needs no journal.
//
// Writers after the build call journalKeep, or for values holding pointers
// journalKeepString or journalKeepSpans, just before they write. Entries go back last first, so a field
// written twice ends with its built value. ui's tests check after each
// phase of each frame that the fields the view built, with the journal
// rolled back, are as it built them (builtcheck_test.go).
type journal struct {
	on      bool
	entries []journalEntry
	data    []byte
	// What the values kept that hold pointers point to, which data holds
	// as bytes the collector does not see: typed, so that keeping them
	// allocates nothing once the frames warmed the journal up.
	strs  []string
	spans [][]Span
	// nodes are whole elements kept (journalKeepNode).
	nodes []node
}

type journalEntry struct {
	p    unsafe.Pointer
	off  int32 // of the value in data, or in nodes for a whole element
	size int32 // 0 for a whole element
}

// journalKeep journals *p, a value without pointers, before a write after the
// build.
func journalKeep[T any](rt *engine, p *T) {
	if j := &rt.journal; j.on {
		j.add(unsafe.Pointer(p), unsafe.Sizeof(*p))
	}
}

// journalKeepString and journalKeepSpans journal *p, values holding
// pointers, before a write after the build.
func journalKeepString(rt *engine, p *string) {
	if rt.journal.on {
		rt.journal.keepString(p)
	}
}

func journalKeepSpans(rt *engine, p *[]Span) {
	if rt.journal.on {
		rt.journal.keepSpans(p)
	}
}

//go:noinline
func (j *journal) keepString(p *string) {
	j.strs = append(j.strs, *p)
	j.add(unsafe.Pointer(p), unsafe.Sizeof(*p))
}

//go:noinline
func (j *journal) keepSpans(p *[]Span) {
	j.spans = append(j.spans, *p)
	j.add(unsafe.Pointer(p), unsafe.Sizeof(*p))
}

// journalKeepNode journals all of e before writes after the build to many
// of its fields, as a transition or a magic move makes, and the inspector's
// edits: what the frame computes in its own fields goes back with them,
// which it computes again.
func journalKeepNode(e *node) {
	if j := &e.c.rt.journal; j.on {
		j.keepNode(e)
	}
}

//go:noinline
func (j *journal) keepNode(e *node) {
	j.nodes = append(j.nodes, *e)
	j.entries = append(j.entries, journalEntry{unsafe.Pointer(e), int32(len(j.nodes) - 1), 0})
}

// add keeps the size bytes at p; not inlined, so that each write
// journaled costs its caller a test and a call.
//
//go:noinline
func (j *journal) add(p unsafe.Pointer, size uintptr) {
	off := len(j.data)
	j.data = append(j.data, unsafe.Slice((*byte)(p), size)...)
	j.entries = append(j.entries, journalEntry{p, int32(off), int32(size)})
}

// start begins journaling, as the build ends.
func (j *journal) start() { j.on = true }

// reset forgets the journal, as a build begins: its elements are built
// anew.
func (j *journal) reset() {
	j.on = false
	j.entries, j.data = j.entries[:0], j.data[:0]
	clear(j.strs)
	clear(j.spans)
	clear(j.nodes)
	j.strs, j.spans, j.nodes = j.strs[:0], j.spans[:0], j.nodes[:0]
}

// rollback puts the fields journaled back as the view built them.
func (j *journal) rollback() {
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if e.size == 0 {
			*(*node)(e.p) = j.nodes[e.off]
			continue
		}
		copy(unsafe.Slice((*byte)(e.p), e.size), j.data[e.off:e.off+e.size])
	}
}
