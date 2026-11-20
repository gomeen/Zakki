package app

// 顶部居中 toast 提示：一次性操作反馈（标签上限、替换计数、导出成功等）
// 以深色气泡统一显示在窗口顶部中央，停留数秒后自动消失。持久异常
// （保存/载入被暂停）不走 toast，仍由状态栏常驻展示，见 view_status.go。

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// showToast 排队一条居中提示；只能在主线程调用（动作回调或 Window.Update
// 回调内）。消息在主窗口下一次构建时经 MyGo toast 引擎展示：默认 4 秒后
// 自动消失，指针悬停或窗口失焦时计时暂停，相同文案替换既有条目重新计时。
func (a *app) showToast(msg string) {
	if msg == "" {
		return
	}
	a.toasts.pending = append(a.toasts.pending, msg)
	// 排队可能发生在不触发重绘的路径上，补一帧保证提示尽快出现。
	if a.win != nil {
		a.win.Update(func() {})
	}
}

// toastViewport 消费排队的提示并构建 toast 视口；主窗口每帧恰好调用一次。
// 视口铺满整窗且指针穿透，条目水平居中、垂直靠顶（避开自绘顶栏）；
// 计时、淡出、Esc 关闭与屏幕阅读器播报都由 MyGo toast 引擎
// （ui.ToastViewportBase / ui.ToastBase）负责。
func (a *app) toastViewport(c *ui.Context) {
	for _, msg := range a.toasts.pending {
		// 文案即 ID：连续触发同一条提示时替换既有条目并重新计时，不层层堆叠。
		c.AddToast(ui.Toast{ID: msg, Title: msg})
	}
	a.toasts.pending = a.toasts.pending[:0]
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		t := c.Theme()
		w, _ := c.Size()
		// 顶部居中：避开 38pt 自绘顶栏（Windows 隐藏标题栏模式同高带 32），
		// 与顶栏拉开一点呼吸。
		viewport.Justify(ui.Start).AlignItems(ui.Center).Gap(t.Space(2)).Padding(46, 0, 0, 0)
		fill, text := toastInk(t)
		for _, ts := range toasts {
			p := ui.ToastBase(c, ts)
			box := p.Root.Column().AlignItems(ui.Center).Justify(ui.Center).
				MaxWidth(w-t.Space(12)).
				Padding(t.Space(3.5), t.Space(5)).
				Radius(t.Space(3)).
				Background(fill).TextColor(text)
			box.Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.22))
			// 收尾 200ms 淡出，与主题默认 toast 一致。
			const fade = 200 * time.Millisecond
			if left := p.Left(); left > 0 && left < fade {
				box.Opacity(float32(left) / float32(fade))
				c.AnimationFrame()
			}
			box.Children(func() {
				ui.Text(c, ts.Title).TextAlign(ui.Center).FontWeight(600)
			})
		}
	})
}

// toastInk 提示气泡的底色与文字色：主题反色（Inverse），未设置时回退
// 文字色作底、窗口背景作文字，与主题为 tooltip/toast 准备的缺省一致。
func toastInk(t *ui.Theme) (fill, text ui.Color) {
	fill, text = t.Inverse, t.InverseText
	if fill == ui.Transparent {
		fill = t.Text
	}
	if text == ui.Transparent {
		text = t.Background
	}
	return fill, text
}
