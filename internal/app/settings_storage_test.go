package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"zakki/internal/notes"
)

func TestStoragePageRendersFileAndGitControls(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.settingsPage = pageStorage
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	for _, want := range []string{"笔记文件", a.notes.DataPath(), "显示文件", "更换位置…"} {
		if !tt.HasText(want) {
			t.Fatalf("保存页缺少 %q: %v", want, tt.Texts())
		}
	}
	// 普通文件夹不渲染 Git 区块：断言 Git 区块独有的文案（云同步提示
	// 也会提到「自动提交」，不再作为区块判定）。
	for _, gone := range []string{"提交间隔", "最近提交"} {
		if tt.HasText(gone) {
			t.Fatalf("非 git 仓库不应显示 %q: %v", gone, tt.Texts())
		}
	}
	if a.prefs.Git.AutoCommit || a.prefs.Git.AutoPush || a.prefs.Git.CommitMinutes != 1 || a.prefs.Git.PushMinutes != 10 {
		t.Fatalf("Git 默认值错误: %+v", a.prefs.Git)
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.applyStorageDir(repo)
	if err := a.updatePreferences(func(p *preferences) { p.Git.AutoCommit = true }); err != nil {
		t.Fatal(err)
	}
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt = ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	for _, want := range []string{"自动提交", "自动推送"} {
		if !tt.HasText(want) {
			t.Fatalf("保存页缺少 Git 选项 %q: %v", want, tt.Texts())
		}
	}
	if a.prefs.Git.AutoPush {
		t.Fatal("开启自动提交不应连带开启推送")
	}
}

func TestStoragePageDraftAppliesWithValidation(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.applyStorageDir(t.TempDir())
	path := a.notes.DataPath()
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	a.settingsUI.draft.Storage.SaveIntervalMS = 3000
	a.settingsUI.draft.Git.AutoCommit = true
	a.settingsUI.draft.Git.AutoPush = false
	a.settingsUI.draft.Git.CommitMinutes = 2
	a.settingsUI.draft.Git.PushMinutes = 30
	a.pushDraft(func(p *preferences) {
		p.Storage.SaveIntervalMS = a.settingsUI.draft.Storage.SaveIntervalMS
		p.Git = a.settingsUI.draft.Git
	})
	if a.prefs.Storage.SaveIntervalMS != 3000 || !a.prefs.Git.AutoCommit || a.prefs.Git.AutoPush || a.prefs.Git.CommitMinutes != 2 || a.prefs.Git.PushMinutes != 30 {
		t.Fatalf("草稿未应用: storage=%+v git=%+v", a.prefs.Storage, a.prefs.Git)
	}
	wantStorage, wantGit := a.prefs.Storage, a.prefs.Git
	for _, field := range []string{"commit", "push", "save"} {
		a.settingsUI.syncDraftFromPrefs(a.prefs)
		switch field {
		case "commit":
			a.settingsUI.draft.Git.CommitMinutes = 0
		case "push":
			a.settingsUI.draft.Git.PushMinutes = 0
		case "save":
			a.settingsUI.draft.Storage.SaveIntervalMS = -1
		}
		a.pushDraft(func(p *preferences) {
			p.Storage.SaveIntervalMS = a.settingsUI.draft.Storage.SaveIntervalMS
			p.Git = a.settingsUI.draft.Git
		})
		if a.settingsUI.keyErr == "" {
			t.Fatalf("%s 非法间隔应提示错误", field)
		}
		if a.prefs.Storage != wantStorage || a.prefs.Git != wantGit {
			t.Fatalf("拒绝非法值时不应部分应用: %+v %+v", a.prefs.Storage, a.prefs.Git)
		}
	}
	if a.notes.DataPath() != path {
		t.Fatal("调整保存和 Git 参数不得改变数据路径")
	}
}

func TestStoragePageGitStatusLine(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.notes.RunGit = func(dir string, args ...string) (string, error) {
		if args[0] == "rev-parse" {
			// 数据路径已规范化（macOS /var → /private/var），根目录必须同形。
			return canonicalPath(dir)
		}
		return "", nil
	}
	a.applyStorageDir(repo)
	if err := a.updatePreferences(func(p *preferences) { p.Git.AutoCommit = true; p.Git.AutoPush = true }); err != nil {
		t.Fatal(err)
	}
	// 缩短测试调度周期，不等待真实的 10 分钟默认推送间隔。
	a.notes.ConfigureGit(notes.GitConfig{AutoCommit: true, AutoPush: true, PushInterval: time.Millisecond, CommitInterval: time.Minute}, nil)
	deadline := time.Now().Add(2 * time.Second)
	for a.notes.GitStatus().PushTime.IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.notes.GitStatus().PushTime.IsZero() {
		t.Fatal("假 runner 应完成一次推送并发布状态")
	}
	a.settingsPage = pageStorage
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 800)
	tt.Frame()
	if !tt.HasText("06-01 12:00") {
		t.Fatalf("状态行应显示最近推送时间: %v", tt.Texts())
	}
}
