//go:build windows && (amd64 || arm64)

package text

import "slices"

// DirectWrite's wrapping from one line (wrap.go). DirectWrite lays a text out
// again at each width, a text layout of its own with its runs analyzed and
// shaped anew. A plain Latin text wrapped across widths is shaped once into
// one line instead, kept while frames wrap it, and its lines take their
// glyphs from it. A line DirectWrite could break otherwise has the whole text
// laid out by DirectWrite.

// wholeKey is a text of a text format (formatKey), which its hash stands for.
type wholeKey struct {
	format formatKey
	text   uint64
}

// keptWhole is a text shaped into one line, nil when it does not come out
// as one run with a glyph for each rune, kept across frames.
type keptWhole struct {
	text  []rune
	whole *shapedWhole
	used  uint64
}

// keptWholeFrames is how many frames a text stays kept unused: windows take
// turns, so a text wrapping again in each frame of its window may wait for
// the others'.
const keptWholeFrames = 8

// wrapsFromWhole reports whether text of a style and spans, wrapped to width
// in a direction, may be wrapped from its one line.
func wrapsFromWhole(text []rune, style Style, spans []Span, width float32, rtl, wholeWords bool) bool {
	return width > 0 && len(spans) == 0 && !rtl && !wholeWords && style.tabStop <= 0 && style.LetterSpacing == 0 && style.Features == "" &&
		!keepsAnyWords(style.Lang, nil) && plainLatin(text) && !languageShapes(text, style.Lang)
}

// wrapWhole wraps text from its one line, or returns nil for DirectWrite to
// wrap it.
func (e *dwrite) wrapWhole(text []rune, style Style, width float32) []shapedLine {
	family, custom := e.family(style.Family)
	format := formatKey{style.Family, family, custom, style.weight(), style.Italic, style.FontSize(), style.Lang}
	f := e.wrapFonts[format]
	if f == nil {
		f = &wrapFont{}
		e.wrapFonts[format] = f
	}
	if f.disagreed {
		return nil
	}
	key := wholeKey{format, textHash(e.textSeed, text)}
	kept := e.wholes[key]
	// Texts of one hash are rare: one takes the other's place.
	if kept == nil || !slices.Equal(kept.text, text) {
		kept = &keptWhole{text: slices.Clone(text), whole: e.shapeWhole(text, style)}
		e.wholes[key] = kept
	}
	kept.used = e.frame
	w := kept.whole
	if w == nil {
		return nil
	}
	n := len(text)
	// As shapeText wraps.
	limit := float64(width) + 1e-3
	var lines []shapedLine
	mark := len(e.shapeScratch.lines)
	for start := 0; start < n; {
		end, pen, plain := start, w.pens[start], true
		last := rune(0) // the last character of the last word that fits
		for i := start; i < n; {
			// A word and the spaces after it, which take no room.
			word := i
			for i < n && text[i] != ' ' {
				i++
			}
			fits := w.pens[i]-pen <= limit
			plain = plainWord(text[word:i])
			if !fits {
				break
			}
			if i > word {
				last = text[i-1]
			}
			for i < n && text[i] == ' ' {
				i++
			}
			end = i
		}
		if end == start || end < n && (!plain || !breaksAcross(last, text[end])) {
			// The first word does not fit, the next may break inside, or
			// the spaces before it may not break.
			return nil
		}
		// DirectWrite shapes the whole text before breaking it into lines,
		// whose glyphs keep their advances, and draws the spaces ending a
		// line as a run of their own.
		glyphs := e.glyphRoom(end - start)
		for i := start; i < end; i++ {
			glyphs[i-start] = Glyph{Font: w.font, ID: w.ids[i], X: float32(w.pens[i] - pen), Advance: w.advances[i], Cluster: i}
		}
		spaces := end
		for spaces > start && text[spaces-1] == ' ' {
			spaces--
		}
		var runs []shapedRun
		runMark := len(e.shapeScratch.runs)
		if spaces > start {
			runs = e.addRun(runMark, shapedRun{font: w.font, start: start, end: spaces, glyphs: glyphs[: spaces-start : spaces-start]})
		}
		if spaces < end {
			runs = e.addRun(runMark, shapedRun{font: w.font, start: spaces, end: end, glyphs: glyphs[spaces-start:]})
		}
		lines = e.addLine(mark, shapedLine{start: start, end: end, runs: runs})
		start = end
	}
	f.lines++
	if f.checked < wrapCheckedFirst || f.lines%wrapCheckedEvery == 0 {
		shaped := e.shapeText(text, style, nil, width, false, false)
		if !sameLines(lines, shaped) {
			f.disagreed = true
			return shaped
		}
		f.checked++
	}
	return lines
}

// breaksAcross reports whether a line surely breaks at the spaces between a
// word ending with last and one starting with next, both plain, which most
// do. Unicode's line breaking keeps an opening bracket with what follows it
// across spaces, and a quotation mark with an opening bracket after it
// (UAX #14, LB14 and LB15).
func breaksAcross(last, next rune) bool {
	switch last {
	case '(', '[', '{':
		return false
	case '"', '\'':
		return next != '(' && next != '[' && next != '{'
	}
	return true
}

// shapeWhole shapes text into one line, or returns nil if it does not come
// out in one font with a glyph for each rune, from the left: in one run, or
// two when it ends with spaces, which DirectWrite draws apart.
func (e *dwrite) shapeWhole(text []rune, style Style) *shapedWhole {
	lines := e.shapeText(text, style, nil, 0, false, false)
	n := len(text)
	if len(lines) != 1 || len(lines[0].runs) == 0 || lines[0].runs[0].font == nil {
		return nil
	}
	font := lines[0].runs[0].font
	w := &shapedWhole{font: font, ids: make([]uint32, n), advances: make([]float32, n), pens: make([]float64, n+1)}
	i := 0
	for _, run := range lines[0].runs {
		if run.font != font || run.start != i {
			return nil
		}
		for _, g := range run.glyphs {
			if i == n || g.Cluster != i || g.Y != 0 || g.RTL || g.Font != font {
				return nil
			}
			w.ids[i], w.advances[i], w.pens[i] = g.ID, g.Advance, float64(g.X)
			i++
		}
		if run.end != i {
			return nil
		}
	}
	if i != n {
		return nil
	}
	w.pens[n] = w.pens[n-1] + float64(w.advances[n-1])
	return w
}

// endFrame lets go of the texts that stopped wrapping again.
func (e *dwrite) endFrame() {
	e.frame++
	for k, t := range e.wholes {
		if e.frame-t.used > keptWholeFrames {
			delete(e.wholes, k)
		}
	}
}

// releaseShaping lets go of all texts kept, as windows go idle.
func (e *dwrite) releaseShaping() { clear(e.wholes) }
