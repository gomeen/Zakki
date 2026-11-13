//go:build windows && (amd64 || arm64)

package text

// DirectWrite's numbers, put together from their characters (numbers.go).

// shapeNumber puts text together if it is a number, or returns nil for
// DirectWrite to shape it.
func (e *dwrite) shapeNumber(text []rune, style Style, spans []Span, width float32, rtl bool) []shapedLine {
	if !mayBeNumber(text, style, spans, rtl) {
		return nil
	}
	// The text format's key is what DirectWrite shapes with: the family
	// list, whose others its fallback tries, the family, weight, style,
	// size and language.
	family, custom := e.family(style.Family)
	key := formatKey{style.Family, family, custom, style.weight(), style.Italic, style.FontSize(), style.Lang}
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

// shapeOne has DirectWrite shape text into one line.
func (e *dwrite) shapeOne(text []rune, style Style) []shapedLine {
	return e.shapeText(text, style, nil, 0, false, false)
}
