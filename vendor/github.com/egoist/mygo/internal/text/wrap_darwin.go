package text

// Core Text's wrapping from one line (wrap.go). Core Text finds line breaks
// a line at a time, opening a line for each and shaping it into glyphs
// through a call for each of its properties. But a line of Latin text is a run
// of the glyphs of the whole text, moved back to the start of the line. So a
// text kept across widths (keptTypesetter) is shaped once into one line, whose
// glyphs its lines take, as GPUI does.
//
// Lines break where Core Text surely would: at the spaces before plain words.
// What could tell them apart goes to Core Text, line by line, or whole: all but
// plain Latin text, and a line ending where the next word does not fit and may
// break inside itself, or is too long for a line. And the first texts of a font,
// and one in every few hundred after them, are also wrapped by Core Text: a font
// where the two differ is left to it from then on.

// shapeWhole shapes a kept text into one line, or returns nil if it does not
// come out as one run with a glyph for each rune.
func (e *coreText) shapeWhole(typesetter uintptr, text []rune, italic bool) *shapedWhole {
	n := len(text)
	line := ct.typesetterCreateLine(typesetter, cfRange{0, n})
	if line == 0 {
		return nil
	}
	defer ct.release(line)
	runs := ct.lineGetGlyphRuns(line)
	if ct.arrayGetCount(runs) != 1 {
		return nil
	}
	run := ct.arrayGetValueAtIndex(runs, 0)
	if ct.runGetGlyphCount(run) != n || ct.runGetStatus(run)&ctRunStatusRightToLeft != 0 {
		return nil
	}
	attrs := ct.runGetAttributes(run)
	w := &shapedWhole{
		font:     e.runFont(ct.dictionaryGetValue(attrs, ct.fontAttributeName), italic || ct.dictionaryGetValue(attrs, e.italic) != 0),
		ids:      make([]uint32, n),
		advances: make([]float32, n),
		pens:     make([]float64, n+1),
	}
	e.runIDs = growTo(e.runIDs, n)
	e.runPoints = growTo(e.runPoints, n)
	e.runAdvances = growTo(e.runAdvances, n)
	e.runIndices = growTo(e.runIndices, n)
	all := cfRange{}
	ct.runGetGlyphs(run, all, &e.runIDs[0])
	ct.runGetPositions(run, all, &e.runPoints[0])
	ct.runGetAdvances(run, all, &e.runAdvances[0])
	ct.runGetStringIndices(run, all, &e.runIndices[0])
	for i := range n {
		if e.runIndices[i] != i || e.runPoints[i].y != 0 {
			return nil
		}
		w.ids[i] = uint32(e.runIDs[i])
		w.advances[i] = float32(e.runAdvances[i].w)
		w.pens[i] = e.runPoints[i].x
	}
	w.pens[n] = w.pens[n-1] + e.runAdvances[n-1].w
	return w
}

// wrapKept wraps a kept text from its one line, or returns nil for Core Text to
// wrap it.
func (e *coreText) wrapKept(kept *keptTypesetter, text []rune, style Style, width float32) []shapedLine {
	f := e.wrapFonts[kept.font]
	if f == nil {
		f = &wrapFont{}
		e.wrapFonts[kept.font] = f
	}
	if f.disagreed {
		return nil
	}
	if !kept.shaped {
		kept.shaped = true
		kept.whole = e.shapeWhole(kept.typesetter, text, style.Italic)
	}
	w := kept.whole
	if w == nil {
		return nil
	}
	if !f.spaceKnown {
		f.spaceKnown = true
		f.space = -1
		if run := oneRun(e.shapeText([]rune{' '}, Style{Size: style.Size, Family: style.Family, Weight: style.Weight, Italic: style.Italic}, nil, 0, false, false)); run != nil && len(run.glyphs) == 1 {
			f.space = run.glyphs[0].Advance
		}
	}
	if f.space < 0 {
		return nil
	}
	n := len(text)
	limit := float64(width) + 1.0/1024
	var lines []shapedLine
	mark := len(e.shapeScratch.lines)
	for start := 0; start < n; {
		end, pen, plain := start, w.pens[start], true
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
			for i < n && text[i] == ' ' {
				i++
			}
			end = i
		}
		if end == start || end < n && !plain {
			// The first word does not fit, or the next may break inside.
			count := max(ct.suggestLineBreak(kept.typesetter, start, limit), 1)
			line := ct.typesetterCreateLine(kept.typesetter, cfRange{start, count})
			if line == 0 {
				return nil
			}
			e.index = runeIndex(e.index, n)
			lines = e.addLine(mark, e.line(line, e.index, n, style.Italic))
			ct.release(line)
			start += count
			continue
		}
		glyphs := e.glyphRoom(end - start)
		for i := start; i < end; i++ {
			glyphs[i-start] = Glyph{Font: w.font, ID: w.ids[i], X: float32(w.pens[i] - pen), Advance: w.advances[i], Cluster: i}
		}
		if end < n {
			glyphs[end-start-1].Advance = f.space
		}
		line := shapedLine{start: start, end: end, runs: e.addRun(len(e.shapeScratch.runs), shapedRun{font: w.font, start: start, end: end, glyphs: glyphs})}
		lines = e.addLine(mark, line)
		start = end
	}
	f.lines++
	if f.checked < wrapCheckedFirst || f.lines%wrapCheckedEvery == 0 {
		e.index = runeIndex(e.index, n)
		shaped := e.typesetLines(kept.typesetter, n, width, nil, false, false, e.index, style.Italic)
		if !sameLines(lines, shaped) {
			f.disagreed = true
			return shaped
		}
		f.checked++
	}
	return lines
}
