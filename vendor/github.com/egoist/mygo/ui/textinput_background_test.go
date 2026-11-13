package ui

import "testing"

func TestTextRangeBackgroundKeepsShapeKey(t *testing.T) {
	ed := newPatchEditor()
	ed.setText("abcdefghij")
	key := func(ranges ...TextRange) string {
		ed.ranges, ed.apart = append(ed.ranges[:0], ranges...), false
		key, _ := ed.rangeSpans(0, 10)
		return key
	}
	weight := TextRange{Start: 2, End: 8, Weight: 700}
	want := key(weight)
	for _, bg := range []TextRange{
		{Start: 0, End: 10, Background: RGB(230, 200, 50)},
		{Start: 3, End: 6, Background: RGB(230, 200, 50)},
		{Start: 8, End: 10, Background: RGB(230, 200, 50)},
	} {
		if got := key(weight, bg); got != want {
			t.Errorf("背景改变形状 key：range=%+v got=%q want=%q", bg, got, want)
		}
	}
	if got := key(TextRange{Start: 1, End: 5, Background: RGB(230, 200, 50)}); got != "" {
		t.Errorf("纯背景产生形状 key：%q", got)
	}
}

func TestTextRangeBackgroundCompositionSplit(t *testing.T) {
	ed := newPatchEditor()
	ed.setText("abcdef")
	ed.caret, ed.compose = 3, "中文😀"
	bg := RGB(230, 200, 50)
	ed.ranges = []TextRange{{Start: 1, End: 5, Background: bg, Weight: 700}}
	_, paint := ed.rangeSpans(0, 6)
	if paint == nil || len(paint.styles) != 4 {
		t.Fatalf("组合文本未拆分原生背景区间：%+v", paint)
	}
	for _, tc := range []struct{ rune, span int }{{1, 1}, {3, 2}, {5, 2}, {6, 3}} {
		index := paint.at(tc.rune)
		if index != tc.span {
			t.Fatalf("rune=%d 的背景区间=%d want=%d", tc.rune, index, tc.span)
		}
		want := bg
		if tc.span == 2 {
			want = Color{}
		}
		if got := paint.spans[index].Background; got != want {
			t.Errorf("rune=%d 背景=%v want=%v", tc.rune, got, want)
		}
	}
}
