package app

import (
	"github.com/egoist/mygo/ui"
)

func (a *app) timeObjectVisual(c *ui.Context, md resolvedStyle, blk liveBlock) ([]ui.Glyph, ui.Color, float32, float32) {
	label := timeBlockLabel(blk, a.now())
	font := ui.Font{Family: md.bodyFont, Size: max(md.bodySize*0.8, 10)}
	glyphs := ui.Shape(label, font)
	var width float32
	for _, g := range glyphs {
		width = max(width, g.X+g.Advance)
	}
	color := c.Theme().TextMuted
	switch timeChipStateFor(blk, a.now()) {
	case chipToday:
		color = md.accent
	case chipOverdue:
		color = c.Theme().Danger
	}
	// 对象高度按正文字体的度量给足：胶囊以小字号绘制，但行盒不能
	// 比普通行矮——否则带时间令牌的行间距明显收紧。
	body := ui.Font{Family: md.bodyFont, Size: md.bodySize}
	m := body.Metrics()
	return glyphs, color, width + 2, m.Ascent + m.Descent
}

func (a *app) timeObjectRanges(c *ui.Context, md resolvedStyle, blk liveBlock, p liveProjection) []ui.TextRange {
	if len(p.objects) == 0 {
		return nil
	}
	_, _, width, box := a.timeObjectVisual(c, md, blk)
	// TextRange 只能表达基线以上的高度（无 descent 字段），盒子总高
	// 按正文度量给足，行盒不再比普通行矮；绘制沿盒子底边即正文基线。
	out := make([]ui.TextRange, 0, len(p.objects))
	for _, at := range p.objects {
		out = append(out, ui.TextRange{Start: at, End: at + 1, ObjectWidth: width, ObjectHeight: box})
	}
	return out
}

func timeObjectSpans(p liveProjection, ranges []ui.TextRange, objects []ui.TextRange) []ui.Span {
	rs := []rune(p.text)
	styles := make([]ui.TextRange, len(rs))
	for _, r := range ranges {
		for i := r.Start; i < r.End && i < len(styles); i++ {
			styles[i] = r
		}
	}
	for _, r := range objects {
		styles[r.Start].ObjectWidth, styles[r.Start].ObjectHeight = r.ObjectWidth, r.ObjectHeight
	}
	var out []ui.Span
	for i := 0; i < len(rs); {
		r := styles[i]
		r.Start, r.End = 0, 0
		end := i + 1
		for end < len(rs) {
			next := styles[end]
			next.Start, next.End = 0, 0
			if next != r {
				break
			}
			end++
		}
		out = append(out, ui.Span{
			Text: string(rs[i:end]), Color: r.Color, Background: r.Background,
			Weight: r.Weight, Font: r.Font, Italic: r.Italic, Underline: r.Underline,
			WavyUnderline: r.WavyUnderline, DecorationColor: r.DecorationColor,
			ObjectWidth: r.ObjectWidth, ObjectHeight: r.ObjectHeight,
		})
		i = end
	}
	return out
}

// paintTimeObjects 使用控件自身排版出的对象盒子，不另外估算光标或换行位置。
func (a *app) paintTimeObjects(c *ui.Context, el ui.Element, md resolvedStyle, blk liveBlock) {
	glyphs, color, _, _ := a.timeObjectVisual(c, md, blk)
	el.DrawTextObjects(func(p *ui.Painter, object ui.TextObject) {
		// 对象盒子只占基线以上的高度，文字沿盒子底边与正文共用基线。
		p.Glyphs(glyphs, object.Rect.X+1, object.Rect.Y+object.Rect.H, color)
	})
}

func (a *app) hitTimeObject(el ui.Element, blk liveBlock) bool {
	if !blk.hasTime {
		return false
	}
	x, y, _ := el.PointerPosition()
	for _, object := range el.TextObjects() {
		r := object.Rect
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			if a.timePop.open && a.timePop.line == blk.start {
				a.timePop.open = false
			} else {
				a.openTimePopover(blk)
			}
			return true
		}
	}
	return false
}

func (a *app) renderTimeBlock(c *ui.Context, t *ui.Theme, md resolvedStyle, index int, blk liveBlock, hits *[]liveHit) {
	p := a.previewProjection(blk)
	spans := timeObjectSpans(p, p.textRanges(a.highlightQuery(), t, md), a.timeObjectRanges(c, md, blk, p))
	body := func() {
		el := ui.RichText(c, spans...).Grow(1).MinWidth(0)
		font := bodyLiveFont(md)
		switch blk.kind {
		case bkHeading:
			styleHeading(el, blk.level, md)
			font = headingLiveFont(md, blk.level)
		case bkQuote:
			el.TextColor(t.TextMuted)
		}
		a.paintTimeObjects(c, el, md, blk)
		a.bindPreviewClick(el, index, p, font, hits)
		if hits != nil && len(*hits) > 0 {
			(*hits)[len(*hits)-1].spans = spans
		}
	}
	switch blk.kind {
	case bkTask:
		a.taskCheckbox(c, blk, md)
	case bkList:
		listMarker(c, md, blk)
	case bkQuote:
		quoteBar(c, md, uint32(blk.start))
	}
	if blk.kind == bkHeading && md.sketch {
		sketchHeadingFrame(c, md, blk, p.text).Children(body)
	} else {
		body()
	}
}
