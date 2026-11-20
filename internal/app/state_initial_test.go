package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"zakki/locales"
)

func TestInitialContentIncludesTimeGuide(t *testing.T) {
	for _, count := range []int{0, 1, 2, DefaultTabCount, MaxTabCount} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			if err := a.commitFirstRunData(count); err != nil {
				t.Fatal(err)
			}
			wantCount := count
			if count == 0 {
				wantCount = DefaultTabCount
			}
			if len(a.tabs) != wantCount {
				t.Fatalf("不能为说明增加标签数: got %d want %d", len(a.tabs), wantCount)
			}
			if wantCount == 1 {
				want := welcomeTextFor(1) + "\n\n" + timeGuideText()
				if a.tabs[0].Text != want {
					t.Fatalf("单标签应同时保留欢迎和时间说明: %q", a.tabs[0].Text)
				}
			} else {
				if a.tabs[0].Text != welcomeTextFor(wantCount) || a.tabs[1].Text != timeGuideText() || a.tabs[1].Name != locales.TimeGuideTabName() {
					t.Fatalf("第二个标签应含独立时间说明，首个仍为欢迎: %+v", a.tabs[:2])
				}
				for i := 2; i < len(a.tabs); i++ {
					if a.tabs[i].Text != "" {
						t.Fatalf("其余标签应为空: %d %q", i, a.tabs[i].Text)
					}
				}
			}
			if entries := a.collectReminders(); len(entries) != 0 {
				t.Fatalf("使用说明的示例不能产生真实提醒: %+v", entries)
			}
			f := persistReadSaved(t, a.notes.DataPath())
			if !reflect.DeepEqual(f.Tabs, a.snapshotFile().Tabs) {
				t.Fatalf("说明的名称与内容必须完整落盘: %+v", f.Tabs)
			}
		})
	}
}

func TestInitialTimeGuideDoesNotReappearForExistingNotes(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	if err := a.commitFirstRunData(2); err != nil {
		t.Fatal(err)
	}
	a.tabs[0].Text = "自己的笔记"
	a.tabs[1].Name, a.tabs[1].Text = "自定义", ""
	if err := a.saveNow(); err != nil {
		t.Fatal(err)
	}
	want := a.snapshotFile().Tabs
	if err := a.commitFirstRunData(9); err != nil {
		t.Fatal(err)
	}
	if err := a.load(); err != nil {
		t.Fatal(err)
	}
	if got := a.snapshotFile().Tabs; !reflect.DeepEqual(got, want) {
		t.Fatalf("已有笔记不能被重建或补回说明: got %+v want %+v", got, want)
	}
}

func TestTimeGuideExampleIsFencedInAllLanguages(t *testing.T) {
	for _, lang := range []string{"en", "ja", "zh-Hans", "zh-Hant"} {
		t.Run(lang, func(t *testing.T) {
			guide := locales.In(lang).TimeGuideBody()
			if !strings.Contains(guide, "@2026-06-01 10:00 ~1h") {
				t.Fatal("说明应包含源码示例")
			}
			found := false
			for _, block := range liveBlocks(guide) {
				if block.hasTime || block.hasAlert {
					t.Fatalf("说明不能解析为真实时间: %+v", block)
				}
				if block.kind == bkCode && strings.Contains(block.raw, "@2026-06-01 10:00 ~1h") {
					found = true
				}
			}
			if !found {
				t.Fatal("源码示例应放在代码围栏内")
			}
		})
	}
}
