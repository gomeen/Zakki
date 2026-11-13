package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestZakkiIdentity(t *testing.T) {
	if appName != "Zakki" || !strings.HasPrefix(welcomeText, "# Zakki\n") {
		t.Fatal("应用名与欢迎文字应使用 Zakki")
	}
	if dataDirectoryName != "Zakki" || dataFileName != "zakki.json" {
		t.Fatal("数据目录与文件应使用 Zakki 命名")
	}
	raw, err := os.ReadFile("../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Name       string `json:"name"`
		Identifier string `json:"identifier"`
		Updates    struct {
			GitHub    string `json:"github"`
			PublicKey string `json:"publicKey"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.Name != appName || config.Identifier != "com.gomeen.zakki" {
		t.Fatalf("安装包应使用 Zakki 名称与标识: %+v", config)
	}
	if config.Updates.GitHub != "gomeen/Zakki" || config.Updates.PublicKey == "" {
		t.Fatal("安装包必须订阅 Zakki 的签名更新")
	}
}

func TestUpdateMenuAndNativePlugin(t *testing.T) {
	a := newTestApp(t)
	menu := a.createMenu()
	item := menu.ItemByID("check-updates")
	if item == nil || item.Label != "检查更新…" || item.Click == nil {
		t.Fatalf("缺少更新入口: %+v", item)
	}
	if p := updatePlugin(); p.Name != "updater" || p.Setup == nil {
		t.Fatal("必须安装 MyGo 原生更新插件")
	}
	// Language 留空 = 跟随 mygo.App.Locale()，中英文界面各自看到同语言的更新窗口。
	if opts := updateOptions(); opts.Language != "" || !opts.DisableAutomaticChecks {
		t.Fatalf("更新窗口必须跟随应用语言，且不默认联网检查: %+v", opts)
	}
}

func TestQuitSavesBeforeUpdateRelaunch(t *testing.T) {
	a := newTestApp(t)
	a.tabs[0].Text = "重启前未保存的笔记"
	a.prefs.Window.AlwaysOnTop = true
	event := &mygo.QuitEvent{}
	a.finishQuit(event)
	if event.DefaultPrevented() {
		t.Fatal("保存成功不应阻止更新重启")
	}
	raw, err := os.ReadFile(a.notes.DataPath())
	if err != nil {
		t.Fatal(err)
	}
	file, _, err := a.notes.ParseRaw(raw)
	if err != nil || file.Tabs[0].Text != a.tabs[0].Text {
		t.Fatalf("退出前应保存笔记: %+v %v", file, err)
	}
	raw, err = os.ReadFile(a.preferencesPath())
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := decodePreferences(raw)
	if err != nil || !prefs.Window.AlwaysOnTop {
		t.Fatalf("退出前应保存偏好: %+v %v", prefs, err)
	}
	entries, err := os.ReadDir(filepath.Dir(a.notes.DataPath()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 { // zakki.json + preferences.json
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("退出不应产生副本文件: %v", names)
	}
}

func TestQuitFailureCancelsUpdateRelaunchAndAllowsRetry(t *testing.T) {
	for _, failure := range []string{"notes", "preferences"} {
		t.Run(failure, func(t *testing.T) {
			a := newTestApp(t)
			blocker := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(blocker, []byte("保留"), 0o600); err != nil {
				t.Fatal(err)
			}
			prefsPath := a.prefsStore.Path
			if failure == "notes" {
				// 数据路径换成目录：原子写必然失败。
				if err := os.MkdirAll(a.notes.DataPath(), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				a.prefsStore.Path = filepath.Join(blocker, "preferences.json")
			}
			event := &mygo.QuitEvent{}
			a.finishQuit(event)
			if !event.DefaultPrevented() || !strings.Contains(strings.Join(a.toasts.pending, "\n"), "保存失败") {
				t.Fatal("任一种保存失败必须取消退出/重启并提示")
			}
			a.prefsStore.Path = prefsPath
			if failure == "notes" {
				if err := os.Remove(a.notes.DataPath()); err != nil {
					t.Fatal(err)
				}
			}
			a.tabs[0].Text = "取消退出后继续编辑"
			if err := a.saveNow(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(a.notes.DataPath())
			if err != nil {
				t.Fatal(err)
			}
			file, _, err := a.notes.ParseRaw(raw)
			if err != nil || file.Tabs[0].Text != a.tabs[0].Text {
				t.Fatal("取消退出后 Store 必须仍可保存新改动")
			}
			event = &mygo.QuitEvent{}
			a.finishQuit(event)
			if event.DefaultPrevented() {
				t.Fatal("修复写盘问题后应能再次退出")
			}
		})
	}
}

func TestQuitBlockedWhileFileUnreadable(t *testing.T) {
	a := newTestApp(t)
	original := []byte("version 超前的原件内容")
	if err := os.WriteFile(a.notes.DataPath(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.load(); err == nil {
		t.Fatal("高版本文件必须载入失败")
	}
	a.tabs[0].Text = "无法保存的新编辑"
	if err := a.saveNow(); err == nil {
		t.Fatal("写保护期间保存必须失败")
	}
	// 退出必须始终放行：不写笔记文件，原件保持不变，未保存编辑随之放弃。
	event := &mygo.QuitEvent{}
	a.finishQuit(event)
	if event.DefaultPrevented() {
		t.Fatal("原件不可写时也必须允许退出")
	}
	raw, err := os.ReadFile(a.notes.DataPath())
	if err != nil || string(raw) != string(original) {
		t.Fatal("写保护期间不得改动原件")
	}
	entries, err := os.ReadDir(filepath.Dir(a.notes.DataPath()))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pending") || strings.Contains(e.Name(), ".corrupt-") {
			t.Fatalf("不得生成暂存或隔离副本: %s", e.Name())
		}
	}
}
