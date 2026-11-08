package app

// 桌面云同步桥接测试：开关与暂停说明、旧 Git 调度器暂停/恢复、远端
// 应用后的重载、冲突留档管理。全部使用隔离数据目录与假后端，不触网。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"zakki/internal/notes"
	"zakki/locales"
)

// fakeSyncBackend2 是 app 层测试用的假后端（避免与 notes 包内测试耦合）。
type fakeSyncBackend2 struct {
	mu      sync.Mutex
	remote  []byte
	version string
	existed bool
	pushes  int
}

func (f *fakeSyncBackend2) BackendID() string { return "github" }

func (f *fakeSyncBackend2) Pull(context.Context) ([]byte, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.existed {
		return nil, "", false, nil
	}
	return append([]byte(nil), f.remote...), f.version, true, nil
}

func (f *fakeSyncBackend2) Push(_ context.Context, content []byte, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes++
	f.remote, f.version, f.existed = append([]byte(nil), content...), "r-new", true
	return "r-new", nil
}

// fakeTestKeyring 是测试用内存安全存储（替换真实钥匙串访问）。
type fakeTestKeyring struct {
	mu    sync.Mutex
	value []byte
}

func (k *fakeTestKeyring) Load() ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.value == nil {
		return nil, notes.ErrKeyringEmpty
	}
	return append([]byte(nil), k.value...), nil
}

func (k *fakeTestKeyring) Store(v []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.value = append([]byte(nil), v...)
	return nil
}

func (k *fakeTestKeyring) Delete() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.value = nil
	return nil
}

// enableSyncPrefs 打开云同步偏好并应用到运行时（configureSync 会装配
// 真实后端壳，但测试随后用假后端覆盖，凭据 provider 不会执行）。安全
// 存储替换为内存实现，测试永不接触真实钥匙串。
func enableSyncPrefs(t *testing.T, a *app) {
	t.Helper()
	if err := a.updatePreferences(func(p *preferences) {
		p.Sync.Enabled = true
		p.Sync.Backend = syncBackendGitHub
		p.Sync.Repo = "gomeen/notes"
	}); err != nil {
		t.Fatal(err)
	}
}

// attachFakeBackend 用假后端覆盖 Store 的同步配置（覆盖 configureSync
// 装配的真实后端壳，凭据 provider 不会执行）。
func attachFakeBackend(t *testing.T, a *app, be notes.SyncBackend) {
	t.Helper()
	a.notes.ConfigureSync(be, "gomeen/notes", a.syncStatePath(), a.syncRefresh())
	t.Cleanup(func() { a.notes.ConfigureSync(nil, "", "", nil) })
}

// syncDoc 构造单标签磁盘文件。
func syncDoc(text string) savedFile {
	return savedFile{Version: currentDataVersion, Active: 0, Tabs: []savedTab{{ID: 1, Text: text}}}
}

// encodeFileForTest 经一次性 Store 把 File 编成磁盘字节（与落盘同一编码）。
func encodeFileForTest(t *testing.T, f savedFile) []byte {
	t.Helper()
	tmp := t.TempDir()
	s := notes.NewStore(filepath.Join(tmp, "zakki.json"), nil, notesMigrateOptions())
	defer s.Close()
	if _, _, _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNow(f); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.DataPath())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func waitSyncIdleFor(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for a.notes.SyncRunning() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.notes.SyncRunning() {
		t.Fatal("同步轮未在限时内结束")
	}
}

func awaitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待超时")
}

func TestSyncSectionRendersAndValidates(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.settingsPage = pageStorage
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	for _, want := range []string{locales.SyncSection(), locales.SyncEnable(), locales.SyncRepo(), locales.SyncConnect()} {
		if !tt.HasText(want) {
			t.Fatalf("云同步分区缺少 %q: %v", want, tt.Texts())
		}
	}
	// 非法配置被偏好校验拒绝：缺仓库。
	if err := a.updatePreferences(func(p *preferences) { p.Sync.Enabled = true }); err == nil {
		t.Fatal("缺仓库应校验失败")
	}
	if a.prefs.Sync.Enabled {
		t.Fatal("非法配置不应应用")
	}
	enableSyncPrefs(t, a)
	if st := a.notes.SyncStateSnapshot(); !st.Enabled || st.Backend != "github" {
		t.Fatalf("Store 应配置同步: %+v", st)
	}
	// 关闭后 Store 状态复位。
	if err := a.updatePreferences(func(p *preferences) { p.Sync.Enabled = false }); err != nil {
		t.Fatal(err)
	}
	if st := a.notes.SyncStateSnapshot(); st.Enabled {
		t.Fatalf("关闭后状态应复位: %+v", st)
	}
}

func TestSyncPauseKeepsGitPrefsAndRestores(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	// 短周期 Git 自动提交 + 计数 runner。
	if err := a.notes.SaveNow(syncDoc("内容")); err != nil {
		t.Fatal(err)
	}
	// git worker 在后台 goroutine 计数，测试 goroutine 只原子读取。
	var calls atomic.Int64
	a.notes.RunGit = func(dir string, args ...string) (string, error) {
		if args[0] == "rev-parse" {
			calls.Add(1)
			return canonicalPath(dir)
		}
		return "", nil
	}
	a.notes.ConfigureGit(notes.GitConfig{AutoCommit: true, CommitInterval: time.Millisecond}, nil)
	awaitFor(t, func() bool { return calls.Load() > 0 })
	if err := a.updatePreferences(func(p *preferences) { p.Git.AutoCommit = true; p.Git.CommitMinutes = 1 }); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	// 开启云同步：用户 Git 开关保留，旧调度器停止装填。
	enableSyncPrefs(t, a)
	if !a.prefs.Git.AutoCommit {
		t.Fatal("开启云同步不得改写用户的 Git 自动提交开关")
	}
	deadline := time.Now().Add(60 * time.Millisecond)
	for time.Now().Before(deadline) {
		if now := calls.Load(); now > before {
			t.Fatalf("同步开启期间旧 Git 调度器不应运行: calls=%d", now)
		}
		time.Sleep(time.Millisecond)
	}
	// 暂停说明横幅出现在设置页。
	a.settingsPage = pageStorage
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	if !tt.HasText(locales.SyncPausedGitHint()) {
		t.Fatalf("开启同步且有 Git 自动化时应显示暂停说明: %v", tt.Texts())
	}
	// 关闭云同步：按保留配置恢复，偏好从未被改写。
	if err := a.updatePreferences(func(p *preferences) { p.Sync.Enabled = false }); err != nil {
		t.Fatal(err)
	}
	if !a.prefs.Git.AutoCommit {
		t.Fatal("关闭云同步后用户的 Git 开关仍应保留")
	}
	// 恢复后调度器重新装填：把周期调短以便测试观察（与偏好值解耦）。
	a.notes.ConfigureGit(notes.GitConfig{AutoCommit: true, CommitInterval: time.Millisecond}, nil)
	awaitFor(t, func() bool { return calls.Load() > before })
	// 横幅消失。
	tt.Frame()
	if tt.HasText(locales.SyncPausedGitHint()) {
		t.Fatal("关闭云同步后暂停说明应消失")
	}
}

func TestSyncRemoteApplyReloadsNotes(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	be := &fakeSyncBackend2{existed: true, version: "r1", remote: encodeFileForTest(t, syncDoc("基线"))}
	enableSyncPrefs(t, a)
	attachFakeBackend(t, a, be)
	// 与真实启动一致：本地文件先存在（openConfiguredNotes 已载入）。
	if err := a.notes.SaveNow(syncDoc("基线")); err != nil {
		t.Fatal(err)
	}
	// 建立基线：本地等于远端 → 只更新基线，不产生任何动作。
	a.notes.SyncNow()
	waitSyncIdleFor(t, a)
	if st := a.notes.SyncStateSnapshot(); st.Error != "" || st.Conflict != nil {
		t.Fatalf("基线建立失败: %+v", st)
	}
	// 远端更新为不同内容：本地未变 → 采用远端并请求重载。
	be.mu.Lock()
	be.remote, be.version = encodeFileForTest(t, syncDoc("远端新内容")), "r2"
	be.mu.Unlock()
	a.notes.SyncNow()
	waitSyncIdleFor(t, a)
	if !a.notes.SyncNeedsReload() {
		t.Fatal("采用远端后应请求重载")
	}
	// 主线程泵：重载磁盘内容并重排提醒（串联点）。
	a.syncPump()
	if a.notes.SyncNeedsReload() {
		t.Fatal("重载后应解除冻结")
	}
	if got := a.tabs[0].Text; got != "远端新内容" {
		t.Fatalf("界面应载入远端内容: %q", got)
	}
}

func TestSyncConflictAreaButtons(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	be := &fakeSyncBackend2{existed: true, version: "r1", remote: encodeFileForTest(t, syncDoc("基线"))}
	enableSyncPrefs(t, a)
	attachFakeBackend(t, a, be)
	if err := a.notes.SaveNow(syncDoc("基线")); err != nil {
		t.Fatal(err)
	}
	a.notes.SyncNow()
	waitSyncIdleFor(t, a)
	// 制造双向分叉。
	if err := a.notes.SaveNow(syncDoc("本机修改")); err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	be.remote, be.version = encodeFileForTest(t, syncDoc("远端修改")), "r2"
	be.mu.Unlock()
	a.notes.SyncNow()
	waitSyncIdleFor(t, a)
	if a.notes.SyncStateSnapshot().Conflict == nil {
		t.Fatal("应挂起冲突")
	}
	// 设置页冲突区提供持久二选一入口。
	a.settingsPage = pageStorage
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	if !tt.HasText(locales.SyncConflictPending()) {
		t.Fatalf("应显示冲突提示: %v", tt.Texts())
	}
	if err := tt.Click(locales.SyncKeepRemote()); err != nil {
		t.Fatal(err)
	}
	waitSyncIdleFor(t, a)
	if st := a.notes.SyncStateSnapshot(); st.Conflict != nil {
		t.Fatal("保留远端后冲突应解除")
	}
	// 落选的本机版本留档在 conflicts/，命名符合规格格式。
	entries := a.listSyncConflicts()
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name, "zakki-") {
		t.Fatalf("应有一个冲突留档: %+v", entries)
	}
	// 删除留档。
	a.deleteConflictArchive(entries[0].Name)
	if left := a.listSyncConflicts(); len(left) != 0 {
		t.Fatalf("删除后应为空: %+v", left)
	}
}
