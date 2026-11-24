package app

import "github.com/egoist/mygo/ui"

// selectMaxHeight 下拉弹层的最大高度：超出部分在弹层内滚动，
// ui.Select 的弹层不限高，选项多时会被窗口裁掉。
const selectMaxHeight = 300

// boundedSelect 与 ui.Select 同外观、同键盘行为的单选下拉，弹层限高可滚动；
// 打开时由 SelectBase 把当前项滚入视野。
func boundedSelect(c *ui.Context, selected *string, options []string) ui.Element {
	return boundedSelectWithPlaceholder(c, selected, options, "")
}

func boundedSelectWithPlaceholder(c *ui.Context, selected *string, options []string, placeholder string) ui.Element {
	t := c.Theme()
	sel := ui.SelectBase(c, selected)
	selectTrigger(c, sel.Trigger, func() {
		if *selected == "" && placeholder != "" {
			ui.Text(c, placeholder).SingleLine().Shrink(1).TextColor(t.TextMuted)
		} else {
			ui.Text(c, *selected).SingleLine().Shrink(1)
		}
	})
	sel.Popup(func(panel ui.Element) {
		selectPanel(c, panel)
		ui.Scroll(c).FillWidth().MaxHeight(selectMaxHeight).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for _, opt := range options {
					item := sel.Item(opt).FillWidth().Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Radius)
					switch {
					case item.Highlighted():
						item.Background(t.Accent).TextColor(t.AccentText)
					case opt == *selected:
						item.Background(t.Surface)
					}
					item.Children(func() { ui.Text(c, opt).SingleLine() })
				}
			})
		})
	})
	return sel.Trigger
}

// selectTrigger 下拉按钮外观（同 ui.Select）：content 在左，箭头在右。
func selectTrigger(c *ui.Context, b ui.Element, content func()) {
	t := c.Theme()
	bg := t.Surface
	switch {
	case b.Pressed():
		bg = t.SurfacePressed
	case b.Hovered():
		bg = t.SurfaceHover
	}
	b.Padding(t.Space(1.5), t.Space(3.5)).Gap(t.Space(1.5)).Radius(t.Radius).
		Background(bg).TextColor(t.Text).Border(1, t.Border).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).MinWidth(t.Space(35)).
		Children(func() {
			content()
			ui.Box(c).Size(t.Space(2.5), t.Space(2.5)).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
				var path ui.Path
				path.MoveTo(r.X+r.W*0.1, r.Y+r.H*0.3).LineTo(r.X+r.W*0.5, r.Y+r.H*0.7).LineTo(r.X+r.W*0.9, r.Y+r.H*0.3)
				p.StrokePath(&path, 1.5, t.TextMuted)
			})
		})
}

// selectPanel 下拉弹层外观（同 ui.Select）。
func selectPanel(c *ui.Context, panel ui.Element) {
	t := c.Theme()
	panel.Margin(t.Space(1), 0, 0, 0).Padding(t.Space(1)).Radius(t.Radius+2).
		Background(t.Background).Border(1, t.Border).
		Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.18))
}
