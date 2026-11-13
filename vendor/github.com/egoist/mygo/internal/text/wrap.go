package text

import (
	"hash/maphash"
	"math"
	"strings"
	"unicode"
	"unsafe"
)

// Wrapping a text at a new width needs its line breaks, which the system's
// text engine finds as it shapes the text again. But a line of Latin text is
// a run of the glyphs of the whole text, moved back to the start of the line.
// So a text wrapped across widths is shaped once into one line, whose glyphs
// its lines take, as GPUI does: Core Text's (wrap_darwin.go) and
// DirectWrite's (wrap_windows.go).
//
// Lines break where the system surely would: at the spaces before plain
// words. What could tell them apart goes to the system: all but plain Latin
// text, and a line ending where the next word does not fit and may break
// inside itself, or is too long for a line. And the first texts of a font,
// and one in every few hundred after them, are also wrapped by the system: a
// font where the two differ is left to it from then on.

const (
	// wrapCheckedFirst is how many wrappings in a font are checked before it
	// is trusted, and wrapCheckedEvery how often after that.
	wrapCheckedFirst = 16
	wrapCheckedEvery = 256
	// wrapTolerance is how far a position may be from the system's, which
	// hands them over rounded.
	wrapTolerance = 1e-3
)

// wrapFont is what wrapping keeps for a font.
type wrapFont struct {
	// space is the advance of a space with nothing after it, which a space
	// ending a line has; spaceKnown tells that it was measured.
	space      float32
	spaceKnown bool
	// lines is how many texts were wrapped, checked how many of them the
	// system agreed with, and disagreed that it did not with one.
	lines, checked uint64
	disagreed      bool
}

// shapedWhole is a text shaped into one line: for each rune its glyph, its
// advance, and the pen before it, with the pen at the end.
type shapedWhole struct {
	font     *Font
	ids      []uint32
	advances []float32
	pens     []float64
}

// plainLatin reports whether text is ASCII or Latin-1 letters, without tabs and
// other marks line breaking or shaping may treat apart.
func plainLatin(text []rune) bool {
	for _, r := range text {
		if r < 0x20 || r > 0x7E && (r < 0xC0 || r > 0xFF) {
			return false
		}
	}
	return true
}

// plainWord reports whether a line can only break before or after a word:
// letters and digits, perhaps with an apostrophe, which may open with a bracket
// or a quote, and close with them or a mark of punctuation.
func plainWord(word []rune) bool {
	i, n := 0, len(word)
	for i < n && strings.ContainsRune("([{\"'", word[i]) {
		i++
	}
	for n > i && strings.ContainsRune(".,;:!?)]}\"'", word[n-1]) {
		n--
	}
	if i == n {
		return false
	}
	for _, r := range word[i:n] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' {
			return false
		}
	}
	return true
}

func growTo[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// textHash is what a text comes to in the key of what is kept of it, its own
// rather than a string of it, which would take making one for each text at
// each width.
func textHash(seed maphash.Seed, text []rune) uint64 {
	return maphash.Bytes(seed, unsafe.Slice((*byte)(unsafe.Pointer(&text[0])), len(text)*4))
}

// runeIndex returns the identity map of a text of BMP runes, as appendUTF16
// makes it, up to its end.
func runeIndex(index []int, n int) []int {
	index = growTo(index, n+1)
	for i := range index {
		index[i] = i
	}
	return index
}

// sameLines reports whether lines wrapped from one line are those the system
// wrapped.
func sameLines(a, b []shapedLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].start != b[i].start || a[i].end != b[i].end {
			return false
		}
		if len(a[i].runs) != len(b[i].runs) || len(a[i].runs) == 0 {
			return false
		}
		for r := range a[i].runs {
			x, y := &a[i].runs[r], &b[i].runs[r]
			if x.font != y.font || x.start != y.start || x.end != y.end || len(x.glyphs) != len(y.glyphs) {
				return false
			}
			for k := range x.glyphs {
				g, h := &x.glyphs[k], &y.glyphs[k]
				if g.ID != h.ID || g.Font != h.Font || g.Cluster != h.Cluster || g.RTL != h.RTL || g.Y != h.Y ||
					math.Abs(float64(g.X-h.X)) > wrapTolerance || math.Abs(float64(g.Advance-h.Advance)) > wrapTolerance {
					return false
				}
			}
		}
	}
	return true
}
