package text

// Core Text's numbers, put together from their characters (numbers.go).

// numberKey is a font of numbers: italic takes another Font of one CTFont.
type numberKey struct {
	font   uintptr
	italic bool
}

// shapeNumber puts text together if it is a number, or returns nil for Core
// Text to shape it.
func (e *coreText) shapeNumber(text []rune, style Style, spans []Span, width float32, rtl bool) []shapedLine {
	if !mayBeNumber(text, style, spans, rtl) {
		return nil
	}
	font := e.ctFont(style)
	if font == 0 {
		return nil
	}
	key := numberKey{font, style.Italic}
	f := e.numbers[key]
	if f == nil {
		f = &numberFont{}
		e.numbers[key] = f
	}
	// All that is needed, before taking room for the result.
	one, ok := f.measure(text, style, width, e.shapeOne)
	if !ok {
		return nil
	}
	n := len(text)
	glyphs := e.glyphRoom(n)
	f.put(text, glyphs)
	line := shapedLine{end: n, runs: e.addRun(len(e.shapeScratch.runs), shapedRun{font: one, end: n, glyphs: glyphs})}
	lines := e.addLine(len(e.shapeScratch.lines), line)
	if shaped := f.check(lines, text, style, width, func() []shapedLine {
		return e.shapeText(text, style, nil, width, false, false)
	}); shaped != nil {
		return shaped
	}
	return lines
}

// shapeOne has Core Text shape text into one line.
func (e *coreText) shapeOne(text []rune, style Style) []shapedLine {
	return e.shapeText(text, style, nil, 0, false, false)
}
