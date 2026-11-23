package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// 预览块间距：列表/待办项之间一律紧凑（源码有无空行都视为同一列表，
// 空行隔开的延续组与相邻项同距）；跨出列表的边界维持两倍/整块间距；
// 聚焦中间项不得改变任何节距。
func TestLiveBlockGapLayout(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.winOpen = true
	a.prefs.Appearance.Style = styleSketch
	a.tabs[0].Text = "- 甲\n- 乙\n\n- 丙\n尾段"
	a.resyncLive(-1)
	a.mdActive = -1
	tt := ui.NewTester(a.view, 400, 400)
	rect := func(s string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("没有渲染 %s active=%d texts=%v", s, a.mdActive, tt.Texts())
		}
		return r
	}
	// sketch：正文行高 16×1.75=28，blockGap 16，同列表项间距 16×0.25=4。
	row, tight, gap := float32(28), float32(4), float32(16)
	check := func(stage string) {
		t.Helper()
		jia, yi, bing, wei := rect("甲"), rect("乙"), rect("丙"), rect("尾段")
		for _, tc := range []struct {
			name       string
			dist, want float32
		}{
			{"同列表相邻项", yi.Y - jia.Y, row + tight},
			{"空行分隔的列表延续组", bing.Y - yi.Y, row + tight},
			{"无空行的跨块边界", wei.Y - bing.Y, row + gap},
		} {
			if tc.dist != tc.want {
				t.Errorf("%s %s 节距 = %v, want %v", stage, tc.name, tc.dist, tc.want)
			}
		}
	}
	check("预览")
	// 聚焦项自身会渲染成编辑器（标签「内容」，无「乙」文本），聚焦后用相邻项验证节距。
	a.focusBlock(1, 3, 3)
	tt.Frame()
	jia2, bing2, wei2 := rect("甲"), rect("丙"), rect("尾段")
	if d := bing2.Y - jia2.Y; d != row+tight+row+tight {
		t.Errorf("聚焦中间项后 甲→丙 节距 = %v, want %v", d, row+tight+row+gap*2)
	}
	if d := wei2.Y - bing2.Y; d != row+gap {
		t.Errorf("聚焦中间项后 丙→尾段 节距 = %v, want %v", d, row+gap)
	}
	if a.tabs[0].Text != "- 甲\n- 乙\n\n- 丙\n尾段" {
		t.Fatalf("仅聚焦改写了正文：%q", a.tabs[0].Text)
	}
}
