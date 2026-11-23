package app

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTimeObjectFocusAppearance(t *testing.T) {
	for _, prefix := range []string{"", "- [ ] ", "- ", "> ", "## "} {
		for _, width := range []int{360, 740} {
			t.Run(fmt.Sprintf("%s/width=%d", prefix, width), func(t *testing.T) {
				a := newFocusAppearanceApp(t, "sonnet", "", prefix+"报销 @2026-11-03 10:00 附发票\n\n后续段落")
				tt := ui.NewTester(a.view, width, 400)
				next, ok := tt.Find("后续段落")
				if !ok {
					t.Fatal("没有后续段落")
				}
				text, ok := tt.Find(a.previewProjection(a.mdBlocks()[0]).text)
				if !ok {
					t.Fatal("没有时间行正文")
				}
				before := cloneFocusImage(tt.Image())
				a.focusBlock(0, -1, -1)
				tt.Frame()
				caret, ok := tt.TextCaret()
				if !ok {
					t.Fatal("聚焦后没有光标")
				}
				if after, ok := tt.Find("后续段落"); !ok || after != next {
					t.Errorf("聚焦改变后续段落位置：before=%+v after=%+v", next, after)
				}
				region := before.Bounds()
				// 输入框会裁掉左侧悬挂笔画，仅比较两种控件共同可见的内容区域。
				region.Min.X, region.Min.Y = int(math.Ceil(float64(text.X))), int(text.Y)
				region.Max.X, region.Max.Y = width-28, int(next.Y)-2
				if diff := focusPixelDiff(before, tt.Image(), region, caret); diff != 0 {
					t.Errorf("聚焦改变时间组件或正文外观：%d pixels text=%+v caret=%+v", diff, text, caret)
				}
			})
		}
	}
}

func TestTimeObjectTextSharesBaseline(t *testing.T) {
	for _, font := range []string{"", "hand"} {
		for _, size := range []float32{14, 24, 32} {
			for _, lineHeight := range []float32{1.2, 1.8} {
				t.Run(fmt.Sprintf("font=%s/size=%g/line=%g", font, size, lineHeight), func(t *testing.T) {
					a := newFocusAppearanceApp(t, "sonnet", font, "- [ ] 报销 @2026-11-03 10:00 附发票")
					md := a.resolveStyle(-1, false, true)
					md.bodySize, md.lineHeight = size, lineHeight
					blk := a.mdBlocks()[0]
					label := timeChipLabel(a.now(), blk.due, false)
					labelFont := ui.Font{Family: md.bodyFont, Size: max(size*0.8, 10)}
					glyphs := ui.Shape(label, labelFont)
					reference := false
					painted := false
					tt := ui.NewTester(func(c *ui.Context) {
						theme := md.apply(*c.Theme())
						c.SetTheme(&theme)
						ui.Row(c).Fill().Font(md.bodyFont).FontSize(size).LineHeight(lineHeight).Children(func() {
							var hits []liveHit
							a.renderTimeBlock(c, &theme, md, 0, blk, &hits)
							el := c.Resolve(hits[0].ref)
							if reference {
								_, color, _, _ := a.timeObjectVisual(c, md, blk)
								el.DrawTextObjects(func(p *ui.Painter, object ui.TextObject) {
									// 对象盒子底边就是正文基线，小字与正文应共用这条线；
									// 盒高按正文度量给足（带时间的行不收紧间距）。
									p.Glyphs(glyphs, object.Rect.X+1, object.Rect.Y+object.Rect.H, color)
									painted = true
									body := ui.Font{Family: md.bodyFont, Size: size}
									want := body.Metrics().Ascent + body.Metrics().Descent
									if object.Rect.H != want {
										t.Errorf("对象高度应按正文度量给足：got %g want %g", object.Rect.H, want)
									}
								})
							}
						})
					}, 640, 120)
					actual := cloneFocusImage(tt.Image())
					reference = true
					tt.Frame()
					if !painted {
						t.Fatal("没有绘制时间组件")
					}
					if diff := focusPixelDiff(actual, tt.Image(), actual.Bounds()); diff != 0 {
						t.Errorf("时间文字偏离正文基线：%d pixels", diff)
					}
				})
			}
		}
	}
}
