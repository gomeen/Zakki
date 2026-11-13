package ui

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/text"
)

func closeObject(a, b float32) bool { return math.Abs(float64(a-b)) < 0.1 }

func TestTextInputObjectDimensionsMergeAndIME(t *testing.T) {
	ed := newPatchEditor()
	ed.setText("a\uFFFCb")
	e := &node{st: &state{editor: ed}, flags: flagEditable}
	e.TextRanges(TextRange{Start: 0, End: 99, ObjectWidth: 41, ObjectHeight: 20},
		TextRange{Start: 1, End: 2, ObjectWidth: 53}, TextRange{Start: 1, End: 2, Weight: 700})
	key, _ := ed.rangeSpans(0, ed.buf.n)
	want := []text.Span{{End: 1}, {End: 2, Weight: 700, ObjectWidth: 53, ObjectHeight: 20}}
	if got := text.DecodeSpans(key); !slices.Equal(got, want) {
		t.Fatalf("对象合并=%+v want=%+v", got, want)
	}
	for _, caret := range []int{1, 2} {
		ed.caret, ed.compose = caret, "中文😀"
		key, _ := ed.rangeSpans(0, ed.buf.n)
		spans := text.DecodeSpans(key)
		start := 0
		objects := 0
		for _, sp := range spans {
			if sp.ObjectWidth > 0 {
				objects++
				wantStart := 1
				if caret == 1 {
					wantStart += 3
				}
				if start != wantStart || sp.End != start+1 {
					t.Fatalf("IME caret=%d object=[%d,%d) want=[%d,%d)", caret, start, sp.End, wantStart, wantStart+1)
				}
			}
			start = sp.End
		}
		if objects != 1 {
			t.Fatalf("IME 对象数=%d", objects)
		}
	}
	ed.compose = ""
	after, _ := ed.rangeSpans(0, ed.buf.n)
	if after != key {
		t.Fatal("组合结束后布局 key 未恢复")
	}
}

func TestTextObjectsActualLayouts(t *testing.T) {
	for _, kind := range []string{"input", "area", "rich"} {
		t.Run(kind, func(t *testing.T) {
			value := "A\uFFFCB"
			width, height := float32(64), float32(30)
			var ed *editor
			var objects, painted []TextObject
			var box Rect
			var clip Rect
			clicked := false
			tester := coreNewTester(func(c *context) {
				var e *node
				switch kind {
				case "input":
					e = textInputBase(c, &value, false)
				case "area":
					e = textInputBase(c, &value, true)
				default:
					e = coreRichText(c, Span{Text: "A"}, Span{Text: "\uFFFC", ObjectWidth: width, ObjectHeight: height}, Span{Text: "B"}).Selectable()
				}
				e.Width(180).Height(90).Padding(4, 7)
				if kind != "rich" {
					e.TextRanges(TextRange{Start: 1, End: 2, ObjectWidth: width, ObjectHeight: height})
					ed = e.st.editor
				}
				painted = nil
				e.DrawTextObjects(func(p *Painter, o TextObject) {
					painted = append(painted, o)
					clip = p.clip
					p.Fill(o.Rect, RGB(200, 10, 20), 3)
				})
				wrapElement(e).OnClick(func() {
					clicked = true
					if got := e.TextObjects(); len(got) != 1 || !closeObject(got[0].Rect.W, width) {
						t.Fatalf("输入回调必须能查询已布局的对象: %+v", got)
					}
				})
				e.DrawOver(func(_ *Painter, r Rect) { objects, box = e.TextObjects(), r })
			}, 220, 150)
			check := func() {
				t.Helper()
				if len(objects) != 1 || len(painted) != 1 {
					t.Fatalf("对象=%+v painted=%+v", objects, painted)
				}
				o, p := objects[0], painted[0]
				if o.Index != 1 || p.Index != 1 || !closeObject(o.Rect.W, width) || !closeObject(o.Rect.H, height) || !closeObject(p.Rect.X-box.X, o.Rect.X) || !closeObject(p.Rect.Y-box.Y, o.Rect.Y) {
					t.Fatalf("实际对象=%+v painted=%+v box=%+v", o, p, box)
				}
				if kind != "rich" && (!closeObject(clip.W, box.W) || !closeObject(clip.H, box.H)) {
					t.Fatalf("输入裁剪=%+v box=%+v", clip, box)
				}
			}
			check()
			tester.ClickAt(box.X+objects[0].Rect.X+1, box.Y+objects[0].Rect.Y+1)
			if !clicked {
				t.Fatal("未触发对象点击回调")
			}
			width, height = 110, 46
			tester.Frame()
			check()
			if kind != "rich" {
				ed.move(1, false)
				x0 := ed.caretRect(tester.rt.states[ed.client.id]).X
				ed.move(2, false)
				x1 := ed.caretRect(tester.rt.states[ed.client.id]).X
				if !closeObject(x1-x0, width) {
					t.Fatalf("光标宽度=%v want=%v", x1-x0, width)
				}
				ed.move(1, false)
				if next := ed.visualNext(1); next.Index != 2 {
					t.Fatalf("对象后光标=%+v", next)
				}
				ed.move(2, false)
				if prev := ed.visualNext(-1); prev.Index != 1 {
					t.Fatalf("对象前光标=%+v", prev)
				}
				for _, edge := range []struct {
					dx    float32
					index int
				}{{1, 1}, {width - 1, 2}} {
					o := objects[0]
					p := ed.hitPosition(o.Rect.X+edge.dx, o.Rect.Y+o.Rect.H/2)
					if p.Index != edge.index {
						t.Fatalf("命中=%+v want=%d", p, edge.index)
					}
				}
			}
		})
	}
}

func TestTextObjectNativeComposition(t *testing.T) {
	for _, caret := range []int{1, 2} {
		t.Run(string(rune('0'+caret)), func(t *testing.T) {
			value := "a\uFFFCb"
			var ed *editor
			var objects []TextObject
			tester := coreNewTester(func(c *context) {
				e := textInputBase(c, &value, true).Width(240).Height(80).Padding(0)
				for i, r := range []rune(value) {
					if r == '\uFFFC' {
						e.TextRanges(TextRange{Start: i, End: i + 1, ObjectWidth: 45, ObjectHeight: 22})
					}
				}
				ed = e.st.editor
				e.DrawOver(func(_ *Painter, _ Rect) { objects = e.TextObjects() })
			}, 300, 120)
			tester.rt.focused = ed.client.id
			ed.move(caret, false)
			ed.client.SetMarkedText(nil, "中文😀", TextInputRange{Start: 4, End: 4})
			tester.Frame()
			if len(objects) != 1 || objects[0].Index != 1 || value != "a\uFFFCb" {
				t.Fatalf("预编辑改变已提交对象：%q %+v", value, objects)
			}
			if marked, ok := ed.client.MarkedRange(); !ok || marked.End-marked.Start != 4 {
				t.Fatalf("组合区间=%+v ok=%v", marked, ok)
			}
			o := objects[0]
			wantHit := 2
			if caret == 1 {
				wantHit += 4
			}
			if index, ok := ed.client.IndexForPoint(Point{o.Rect.X + o.Rect.W - 1, o.Rect.Y + o.Rect.H/2}); !ok || index != wantHit {
				t.Fatalf("组合对象命中 index=%d ok=%v", index, ok)
			}
			ed.client.ReplaceText(nil, "中")
			tester.Frame()
			want := "a\uFFFC中b"
			wantIndex := 1
			if caret == 1 {
				want, wantIndex = "a中\uFFFCb", 2
			}
			if value != want || len(objects) != 1 || objects[0].Index != wantIndex {
				t.Fatalf("组合提交=%q objects=%+v", value, objects)
			}
			tester.Key(Cmd, KeyZ)
			if value != "a\uFFFCb" || ed.caret != caret {
				t.Fatalf("组合撤销=%q caret=%d", value, ed.caret)
			}
		})
	}
}

func TestTextObjectIndexedGrowingHeightInvalidation(t *testing.T) {
	buffer := NewTextBuffer("x\uFFFC y")
	var ed *editor
	height := float32(20)
	tester := coreNewTester(func(c *context) {
		e := bufferInputBase(c, buffer, true).Width(130).Lines(1, 8)
		e.TextRanges(TextRange{Start: 1, End: 2, ObjectWidth: 40, ObjectHeight: height})
		ed = e.st.editor
	}, 200, 200)
	before := ed.area.wrapped
	height = 64
	tester.Frame()
	if ed.area.wrapped <= before+30 || ed.area.wrapped < 64 {
		t.Fatalf("索引文本增长高度未失效：before=%v after=%v", before, ed.area.wrapped)
	}
}

func TestTextObjectAtomicEditing(t *testing.T) {
	ed := newPatchEditor()
	ed.setText("a\uFFFCb")
	ed.move(2, false)
	ed.insert("x")
	if ed.String() != "a\uFFFCxb" {
		t.Fatalf("对象后输入=%q", ed.String())
	}
	ed.takeBack(false)
	if ed.String() != "a\uFFFCb" || ed.caret != 2 {
		t.Fatalf("撤销输入=%q caret=%d", ed.String(), ed.caret)
	}
	ed.key(nil, &state{}, editEvent{key: KeyBackspace})
	if ed.String() != "ab" || ed.caret != 1 {
		t.Fatalf("删除对象=%q caret=%d", ed.String(), ed.caret)
	}
	ed.takeBack(false)
	if ed.String() != "a\uFFFCb" || ed.caret != 2 {
		t.Fatalf("撤销删除=%q caret=%d", ed.String(), ed.caret)
	}
	ed.takeBack(true)
	if ed.String() != "ab" {
		t.Fatalf("重做删除=%q", ed.String())
	}
	ed.takeBack(false)
	ed.move(1, false)
	ed.key(nil, &state{}, editEvent{key: KeyDelete})
	if ed.String() != "ab" {
		t.Fatalf("向前删除=%q", ed.String())
	}
}

func TestTextObjectParagraphWrapComposeAndIndices(t *testing.T) {
	value := "😀\uFFFC\nxx \uFFFC yy"
	var ed *editor
	var objects []TextObject
	var width float32 = 56
	tester := coreNewTester(func(c *context) {
		e := textInputBase(c, &value, true).Width(90).Height(180).Padding(0)
		e.TextRanges(TextRange{Start: 1, End: 2, ObjectWidth: width, ObjectHeight: 25}, TextRange{Start: 6, End: 7, ObjectWidth: width, ObjectHeight: 25})
		ed = e.st.editor
		e.DrawOver(func(_ *Painter, _ Rect) { objects = e.TextObjects() })
	}, 180, 220)
	check := func() {
		t.Helper()
		if len(objects) != 2 || objects[0].Index != 1 || objects[1].Index != 6 {
			t.Fatalf("段落对象索引=%+v", objects)
		}
	}
	check()
	p := ed.area.paraLayout(ed, 1)
	if len(p.Lines) < 2 {
		t.Fatalf("对象未参与换行：%+v", p.Lines)
	}
	tester.rt.focused = ed.client.id
	for _, caret := range []int{1, 2} {
		ed.move(caret, false)
		ed.client.SetMarkedText(nil, "中文😀", TextInputRange{Start: 4, End: 4})
		tester.Frame()
		check()
		if !strings.Contains(ed.displayText(), "中文😀") {
			t.Fatal("组合文本丢失")
		}
		ed.compose = ""
		tester.Frame()
	}
	before := ed.area.paraLayout(ed, 1)
	width = 20
	tester.Frame()
	check()
	after := ed.area.paraLayout(ed, 1)
	if before == after || !closeObject(objects[1].Rect.W, 20) {
		t.Fatal("对象尺寸改变未失效段落布局")
	}
}
