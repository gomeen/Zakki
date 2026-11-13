package text

import "math"

// A table of prices or a dashboard shows new numbers in every frame, each a
// text the system's text engine has never shaped. But a number in one font is
// its characters side by side, each with its glyph and advance, its advance
// changed by the kerning with the next: an engine measures those once for
// each font, by having the system shape each character and each pair, and
// puts numbers together from them (numberFont). GPUI Fast does the same.
// Core Text (numbers_darwin.go) and DirectWrite (numbers_windows.go) do.
//
// That holds while only neighbours affect each other. So the characters are
// few, a pair the system shapes as anything but its two glyphs is left to it,
// and the first numbers of a font, and one in every few hundred after them,
// are shaped by the system too: a font where the two differ is left to it
// from then on.

// numberCharacters are what numbers are written with.
const numberCharacters = "0123456789.,+-%$"

const (
	numberCount = len(numberCharacters)
	// maxNumber is the longest text put together: longer ones are rarely
	// numbers.
	maxNumber = 32
	// numbersCheckedFirst is how many numbers of a font are checked before
	// it is trusted, and numbersCheckedEvery how often after that.
	numbersCheckedFirst = 32
	numbersCheckedEvery = 256
	// numberTolerance is how far a position may be from the system's, which
	// may add advances up in double precision and hand them over rounded.
	numberTolerance = 1e-3
)

// numberSlots gives a character's place in numberCharacters, plus one.
var numberSlots = func() (slots [128]uint8) {
	for i := range numberCount {
		slots[numberCharacters[i]] = uint8(i + 1)
	}
	return slots
}()

// numberGlyph is what is known of a character alone. usable tells that it is
// one glyph standing on the pen.
type numberGlyph struct {
	font    *Font
	id      uint32
	advance float32
	known   bool
	usable  bool
}

// numberPair is what is known of the first of two characters side by side,
// whose advance has their kerning in it. usable tells that they are their two
// glyphs alone.
type numberPair struct {
	advance float32
	known   bool
	usable  bool
}

// numberFont is what a font shapes numbers into, measured as they come.
type numberFont struct {
	glyphs [numberCount]numberGlyph
	pairs  [numberCount][numberCount]numberPair
	// lines is how many numbers were put together, checked how many of them
	// the system agreed with, and disagreed that it did not with one.
	lines, checked uint64
	disagreed      bool
}

// shapeOne has the system shape text of a style into one line, unwrapped,
// left to right: what a numberFont measures and checks against.
type shapeOne func(text []rune, style Style) []shapedLine

// mayBeNumber reports whether text, of a style and spans, laid out in a
// direction, is one an engine may put together, before its font is known.
func mayBeNumber(text []rune, style Style, spans []Span, rtl bool) bool {
	n := len(text)
	if n == 0 || n > maxNumber || rtl || len(spans) > 0 || style.LetterSpacing != 0 || style.Features != "" || style.tabStop > 0 {
		return false
	}
	for _, r := range text {
		if r >= 128 || numberSlots[r] == 0 {
			return false
		}
	}
	return !languageShapes(text, style.Lang)
}

// measure returns the font a number's glyphs all come from, and whether it
// can be put together and fits width (0 for no limit). Text passed
// mayBeNumber.
func (f *numberFont) measure(text []rune, style Style, width float32, shape shapeOne) (*Font, bool) {
	if f.disagreed {
		return nil, false
	}
	n := len(text)
	var one *Font
	pen := 0.0
	for i, r := range text {
		a := numberSlots[r] - 1
		g := f.glyph(a, style, shape)
		if !g.usable || one != nil && g.font != one {
			return nil, false
		}
		one = g.font
		advance := g.advance
		if i+1 < n {
			p := f.pair(a, numberSlots[text[i+1]]-1, style, shape)
			if !p.usable {
				return nil, false
			}
			advance = p.advance
		}
		pen += float64(advance)
	}
	if width > 0 && pen > float64(width)+1.0/1024 {
		return nil, false // it wraps
	}
	return one, true
}

// put fills glyphs, as long as text, with the number text, which measure
// took.
func (f *numberFont) put(text []rune, glyphs []Glyph) {
	n := len(text)
	x := 0.0
	for i, r := range text {
		a := numberSlots[r] - 1
		g := &f.glyphs[a]
		advance := g.advance
		if i+1 < n {
			advance = f.pairs[a][numberSlots[text[i+1]]-1].advance
		}
		glyphs[i] = Glyph{Font: g.font, ID: g.id, X: float32(x), Advance: advance, Cluster: i}
		x += float64(advance)
	}
}

// check counts a number put together into lines, and has the system shape it
// when it is one of those checked: it returns what the system shaped when the
// two differ, and the font is left to the system from then on, or nil.
func (f *numberFont) check(lines []shapedLine, text []rune, style Style, width float32, shape func() []shapedLine) []shapedLine {
	f.lines++
	if f.checked >= numbersCheckedFirst && f.lines%numbersCheckedEvery != 0 {
		return nil
	}
	shaped := shape()
	if !sameNumber(lines, shaped) {
		f.disagreed = true
		return shaped
	}
	f.checked++
	return nil
}

// glyph returns the measure of a character alone.
func (f *numberFont) glyph(a uint8, style Style, shape shapeOne) *numberGlyph {
	g := &f.glyphs[a]
	if g.known {
		return g
	}
	g.known = true
	shaped := shape([]rune{rune(numberCharacters[a])}, style)
	if run := oneRun(shaped); run != nil && len(run.glyphs) == 1 {
		if s := run.glyphs[0]; s.X == 0 && s.Y == 0 && s.Cluster == 0 && s.Font != nil {
			*g = numberGlyph{font: s.Font, id: s.ID, advance: s.Advance, known: true, usable: true}
		}
	}
	return g
}

// pair returns the measure of the first of two characters side by side,
// whose glyphs must be theirs alone with the second placed by the first's
// advance.
func (f *numberFont) pair(a, b uint8, style Style, shape shapeOne) *numberPair {
	p := &f.pairs[a][b]
	if p.known {
		return p
	}
	p.known = true
	first, second := f.glyph(a, style, shape), f.glyph(b, style, shape)
	if !first.usable || !second.usable {
		return p
	}
	run := oneRun(shape([]rune{rune(numberCharacters[a]), rune(numberCharacters[b])}, style))
	if run == nil || len(run.glyphs) != 2 {
		return p
	}
	x, y := run.glyphs[0], run.glyphs[1]
	if x.ID == first.id && y.ID == second.id && x.Font == first.font && y.Font == second.font && x.X == 0 && x.Y == 0 && y.Y == 0 &&
		x.Cluster == 0 && y.Cluster == 1 && math.Abs(float64(y.X-x.Advance)) <= numberTolerance {
		*p = numberPair{advance: x.Advance, known: true, usable: true}
	}
	return p
}

// oneRun returns the run of lines that are one line of one run, or nil.
func oneRun(lines []shapedLine) *shapedRun {
	if len(lines) != 1 || len(lines[0].runs) != 1 {
		return nil
	}
	return &lines[0].runs[0]
}

// sameNumber reports whether a number put together is what the system
// shaped.
func sameNumber(a, b []shapedLine) bool {
	x, y := oneRun(a), oneRun(b)
	if x == nil || y == nil || x.font != y.font || x.start != y.start || x.end != y.end || len(x.glyphs) != len(y.glyphs) {
		return false
	}
	for i := range x.glyphs {
		g, h := &x.glyphs[i], &y.glyphs[i]
		if g.ID != h.ID || g.Font != h.Font || g.Cluster != h.Cluster || g.RTL != h.RTL || g.Y != h.Y ||
			math.Abs(float64(g.X-h.X)) > numberTolerance || math.Abs(float64(g.Advance-h.Advance)) > numberTolerance {
			return false
		}
	}
	return true
}
