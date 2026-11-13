package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/text"
)

// newPatchEditor 供补丁回归测试构造编辑器；这些测试只看布局与绘制，时钟取零值。
func newPatchEditor() *editor { return newEditor(func() time.Time { return time.Time{} }) }

func TestTextRangeStylesMergeIndependently(t *testing.T) {
	red, blue, yellow := RGB(220, 30, 40), RGB(20, 60, 220), RGB(240, 220, 80)
	base := TextRange{Start: 0, End: 10, Font: "serif", Italic: true, Underline: true, WavyUnderline: true, DecorationColor: red, Color: red, Background: yellow, Weight: 400}
	ed := newPatchEditor()
	ed.ranges = []TextRange{
		base,
		{Start: 2, End: 8, Font: "monospace"},
		{Start: 3, End: 7, Color: blue},
		{Start: 4, End: 6, Weight: 700, Background: blue},
		{Start: 5, End: 9, DecorationColor: blue},
		{Start: 1, End: 9},
	}
	ed.sortRanges()
	want := []TextRange{
		base,
		{Start: 2, End: 3, Font: "monospace"},
		{Start: 3, End: 4, Font: "monospace", Color: blue},
		{Start: 4, End: 5, Font: "monospace", Color: blue, Background: blue, Weight: 700},
		{Start: 5, End: 6, Font: "monospace", Color: blue, Background: blue, Weight: 700, DecorationColor: blue},
		{Start: 6, End: 7, Font: "monospace", Color: blue, DecorationColor: blue},
		{Start: 7, End: 8, Font: "monospace", DecorationColor: blue},
		{Start: 8, End: 9, DecorationColor: blue},
		base,
	}
	want[0].End = 2
	want[len(want)-1].Start = 9
	for i := 1; i+1 < len(want); i++ {
		r := &want[i]
		r.Italic, r.Underline, r.WavyUnderline = true, true, true
		if r.Font == "" {
			r.Font = base.Font
		}
		if r.Color.A == 0 {
			r.Color = base.Color
		}
		if r.Background.A == 0 {
			r.Background = base.Background
		}
		if r.DecorationColor.A == 0 {
			r.DecorationColor = base.DecorationColor
		}
		if r.Weight == 0 {
			r.Weight = base.Weight
		}
	}
	if !slices.Equal(ed.ranges, want) {
		t.Fatalf("合并区间=%+v，期望=%+v", ed.ranges, want)
	}
}

func TestTextRangeLayoutStylesShapeKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rangeStyle TextRange
		style      text.Span
	}{
		{"font", TextRange{Font: "monospace"}, text.Span{Family: "monospace"}},
		{"italic", TextRange{Italic: true}, text.Span{Italic: true}},
		{"weight", TextRange{Weight: 700}, text.Span{Weight: 700}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newPatchEditor()
			r := tc.rangeStyle
			r.Start, r.End = 2, 8
			ed.ranges = []TextRange{r}
			key, paint := ed.rangeSpans(0, 10)
			style := tc.style
			style.End = 8
			want := []text.Span{{End: 2}, style}
			if got := text.DecodeSpans(key); !slices.Equal(got, want) {
				t.Fatalf("布局样式=%+v，期望=%+v", got, want)
			}
			if paint != nil {
				t.Fatalf("纯布局样式产生绘制属性：%+v", paint)
			}
			switch tc.name {
			case "font":
				r.Font = "serif"
			case "italic":
				r.Italic = false
			case "weight":
				r.Weight = 400
			}
			ed.ranges, ed.apart = []TextRange{r}, false
			if changed, _ := ed.rangeSpans(0, 10); changed == key {
				t.Fatal("改变布局属性后 key 未变化")
			}
		})
	}
}

func TestTextRangePaintStylesKeepShapeKey(t *testing.T) {
	ed := newPatchEditor()
	key := func(ranges ...TextRange) string {
		ed.ranges, ed.apart = append(ed.ranges[:0], ranges...), false
		key, _ := ed.rangeSpans(0, 10)
		return key
	}
	color := RGB(20, 80, 200)
	for _, layout := range []TextRange{
		{}, {Font: "monospace"}, {Italic: true}, {Weight: 700},
		{Font: "monospace", Italic: true, Weight: 700},
	} {
		layout.Start, layout.End = 2, 8
		want := key(layout)
		for _, paint := range []TextRange{
			{Color: color}, {Background: color}, {Underline: true},
			{WavyUnderline: true}, {DecorationColor: color},
		} {
			for _, bounds := range [][2]int{{0, 10}, {3, 6}, {8, 10}} {
				paint.Start, paint.End = bounds[0], bounds[1]
				if got := key(layout, paint); got != want {
					t.Errorf("绘制改变布局 key：layout=%+v paint=%+v", layout, paint)
				}
			}
		}
	}
	font, italic := TextRange{Start: 1, End: 4, Font: "monospace"}, TextRange{Start: 4, End: 8, Italic: true}
	want := []text.Span{{End: 1}, {End: 4, Family: "monospace"}, {End: 8, Italic: true}}
	if got := text.DecodeSpans(key(font, italic, TextRange{Start: 3, End: 6, Background: color})); !slices.Equal(got, want) {
		t.Fatalf("绘制拆段丢失布局边界：got=%+v want=%+v", got, want)
	}
}

func TestTextRangeDecorationOnlyPaint(t *testing.T) {
	color := RGB(20, 80, 200)
	for _, r := range []TextRange{
		{Underline: true}, {WavyUnderline: true}, {DecorationColor: color},
	} {
		r.Start, r.End = 1, 4
		ed := newPatchEditor()
		ed.ranges = []TextRange{r}
		key, paint := ed.rangeSpans(0, 6)
		if key != "" || paint == nil {
			t.Fatalf("纯装饰 key=%q paint=%+v，期望无布局 key 且有绘制属性", key, paint)
		}
		want := Span{Underline: r.Underline, WavyUnderline: r.WavyUnderline, DecorationColor: r.DecorationColor}
		if len(paint.styles) != 2 || paint.styles[1].End != 4 || paint.spans[1] != want {
			t.Fatalf("装饰绘制属性丢失：%+v %+v", paint.styles, paint.spans)
		}
	}
}

func TestTextRangeStylesCompositionSplitAndRestore(t *testing.T) {
	color, bg := RGB(20, 80, 200), RGB(240, 220, 80)
	r := TextRange{Start: 2, End: 8, Font: "monospace", Italic: true, Weight: 700, Color: color, Background: bg, Underline: true, WavyUnderline: true, DecorationColor: color}
	styled := Span{Font: r.Font, Italic: true, Weight: 700, Color: color, Background: bg, Underline: true, WavyUnderline: true, DecorationColor: color}
	for _, tc := range []struct {
		name   string
		caret  int
		styles []text.Span
		spans  []Span
	}{
		{"before", 2, []text.Span{{End: 4}, {End: 10, Family: r.Font, Italic: true, Weight: 700}}, []Span{{}, styled}},
		{"inside", 5, []text.Span{{End: 1}, {End: 4, Family: r.Font, Italic: true, Weight: 700}, {End: 7}, {End: 10, Family: r.Font, Italic: true, Weight: 700}}, []Span{{}, styled, {}, styled}},
		{"after", 8, []text.Span{{End: 1}, {End: 7, Family: r.Font, Italic: true, Weight: 700}}, []Span{{}, styled}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newPatchEditor()
			ed.setText("甲乙丙丁戊己庚辛壬癸")
			ed.ranges = []TextRange{r}
			before, _ := ed.rangeSpans(1, 9)
			ed.caret, ed.compose = tc.caret, "中文😀"
			key, paint := ed.rangeSpans(1, 9)
			if got := text.DecodeSpans(key); !slices.Equal(got, tc.styles) {
				t.Fatalf("IME 布局区间=%+v，期望=%+v", got, tc.styles)
			}
			if paint == nil || !slices.Equal(paint.styles, tc.styles) || !slices.Equal(paint.spans, tc.spans) {
				t.Fatalf("IME 绘制区间=%+v，期望 styles=%+v spans=%+v", paint, tc.styles, tc.spans)
			}
			ed.compose = ""
			key, paint = ed.rangeSpans(1, 9)
			if key != before || paint == nil || len(paint.spans) != 2 || paint.spans[1] != styled {
				t.Fatalf("组合结束未恢复样式：key=%q paint=%+v", key, paint)
			}
		})
	}
}
