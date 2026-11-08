package notes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"zakki/core/model"
)

// fakeSyncBackend 是同步轮的假后端：远端内容与版本号显式可控，
// Push 行为可注入（默认 CAS 校验 expect == 当前版本号）。
type fakeSyncBackend struct {
	mu        sync.Mutex
	remote    []byte
	version   string
	existed   bool
	pullErr   error
	pushErr   error
	pullCalls int
	pushCalls int
	pushFn    func(content []byte, expect string) (string, error)
}

func (f *fakeSyncBackend) BackendID() string { return "github" }

func (f *fakeSyncBackend) Pull(context.Context) ([]byte, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullCalls++
	if f.pullErr != nil {
		return nil, "", false, f.pullErr
	}
	if !f.existed {
		return nil, "", false, nil
	}
	return append([]byte(nil), f.remote...), f.version, true, nil
}

func (f *fakeSyncBackend) Push(_ context.Context, content []byte, expect string) (string, error) {
	f.mu.Lock()
	f.pushCalls++
	fn, err := f.pushFn, f.pushErr
	f.mu.Unlock()
	if err != nil {
		return "", err
	}
	if fn != nil {
		return fn(content, expect)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if expect != f.version {
		return "", ErrSyncConflict
	}
	f.remote, f.version, f.existed = append([]byte(nil), content...), "v-"+fmt.Sprint(f.pushCalls), true
	return f.version, nil
}

func (f *fakeSyncBackend) snapshot() (remote []byte, version string, existed bool, pulls, pushes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.remote...), f.version, f.existed, f.pullCalls, f.pushCalls
}

func newSyncStore(t *testing.T, initial string) (*Store, *fakeSyncBackend, string) {
	t.Helper()
	s := newTestStore(t, nil)
	if err := s.SaveNow(doc(initial)); err != nil {
		t.Fatal(err)
	}
	backend := &fakeSyncBackend{existed: false, version: ""}
	stateFile := filepath.Join(filepath.Dir(s.DataPath()), "sync-state.json")
	t.Cleanup(func() { s.ConfigureSync(nil, "", "", nil) })
	return s, backend, stateFile
}

func waitSyncIdle(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for s.SyncRunning() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.SyncRunning() {
		t.Fatal("同步轮未在限时内结束")
	}
}

// startSync 配置并触发一轮同步，等它结束并返回状态。
func startSync(t *testing.T, s *Store, backend SyncBackend) SyncState {
	t.Helper()
	s.ConfigureSync(backend, "gomeen/notes", filepath.Join(filepath.Dir(s.DataPath()), "sync-state.json"), nil)
	s.SyncNow()
	waitSyncIdle(t, s)
	return s.SyncStateSnapshot()
}

func readBaseline(t *testing.T, s *Store) syncBaselineV1 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(s.DataPath()), "sync-state.json"))
	if err != nil {
		t.Fatalf("读取基线失败: %v", err)
	}
	var base syncBaselineV1
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("基线解析失败: %v", err)
	}
	return base
}

// TestSyncDecisionTable D13 判定表逐条：基线比较只看内容指纹与存在性。
func TestSyncDecisionTable(t *testing.T) {
	local, remote := "h-local", "h-remote"
	synced := syncBaselineV1{Version: 1, Backend: "github", ContentSHA256: "base", RemoteVersion: "r1"}
	never := syncBaselineV1{Version: 1}
	cases := []struct {
		name       string
		localExist bool
		localHash  string
		remExist   bool
		remHash    string
		base       syncBaselineV1
		directive  *syncDirective
		want       syncDecision
	}{
		{"双方未变", true, "base", true, "base", synced, nil, syncNone},
		{"本地等于基线远端已变→采用远端", true, "base", true, remote, synced, nil, syncAdoptRemote},
		{"本地已变远端等于基线→上传", true, local, true, "base", synced, nil, syncUploadLocal},
		{"双方已变内容相同→只更基线", true, local, true, local, synced, nil, syncUpdateBaseline},
		{"双方已变内容不同→冲突", true, local, true, remote, synced, nil, syncNeedResolve},
		{"首同步本地远端都有→冲突", true, local, true, remote, never, nil, syncNeedResolve},
		{"首同步远端缺失→上传", true, local, false, "", never, nil, syncUploadLocal},
		{"首同步本地缺失→采用远端", false, "", true, remote, never, nil, syncAdoptRemote},
		{"首同步双方都无→无动作", false, "", false, "", never, nil, syncNone},
		{"基线后远端丢失→采用远端(删除)", true, "base", false, "", synced, nil, syncAdoptRemote},
		{"裁决保留本机→上传", true, local, true, remote, synced, &syncDirective{keepLocal: true}, syncUploadLocal},
		{"裁决保留远端→采用远端", true, local, true, remote, synced, &syncDirective{keepLocal: false}, syncAdoptRemote},
		{"本地丢失远端未变→恢复远端", false, "", true, "base", synced, nil, syncAdoptRemote},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideSync(tc.localExist, tc.localHash, tc.remExist, tc.remHash, tc.base, tc.directive)
			if got != tc.want {
				t.Fatalf("decideSync = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSyncAdoptRemote 情况一：本地等于基线、远端已变 → 远端安全落盘、
// 基线更新、界面请求重载、冻结期间保存被拒、重载解除。
func TestSyncAdoptRemote(t *testing.T) {
	s, be, _ := newSyncStore(t, "本地内容")
	// 建立基线：本地已变、远端等于基线 → 上传，基线落盘。
	be.version = "r1"
	be.existed, be.remote = true, docBytes("本地内容")
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatalf("建立基线失败: %s", st.Error)
	}
	if base := readBaseline(t, s); base.ContentSHA256 != contentHash(docBytes("本地内容")) {
		t.Fatalf("基线内容指纹错误: %+v", base)
	}
	// 远端变化：本地未变 → 采用远端。
	be.mu.Lock()
	be.remote, be.version = docBytes("远端新内容"), "r2"
	be.mu.Unlock()
	if st := startSync(t, s, be); st.Error != "" || st.LastRemoteApply.IsZero() {
		t.Fatalf("采用远端失败: %+v", st)
	}
	got, err := os.ReadFile(s.DataPath())
	if err != nil || !bytes.Equal(got, docBytes("远端新内容")) {
		t.Fatalf("远端内容未落盘: %q %v", got, err)
	}
	if !s.SyncNeedsReload() {
		t.Fatal("采用远端后应请求界面重载")
	}
	// 冻结期间：ScheduleSave 丢弃、SaveNow 拒绝、内存未保存状态清零。
	s.ScheduleSave(doc("迟到的输入"), time.Hour, nil)
	if err := s.SaveNow(doc("另一笔")); !errors.Is(err, ErrSyncHold) {
		t.Fatalf("冻结期 SaveNow 应返回 ErrSyncHold: %v", err)
	}
	if s.Unsaved() {
		t.Fatal("冻结期不应保留未保存状态")
	}
	// 显式重载解除冻结并把远端内容带回内存。
	f, _, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Tabs) == 0 || f.Tabs[0].Text != "远端新内容" {
		t.Fatalf("重载后内存应为远端内容: %+v", f)
	}
	if s.SyncNeedsReload() {
		t.Fatal("重载后应解除冻结")
	}
	if base := readBaseline(t, s); base.RemoteVersion != "r2" || base.RemoteNil {
		t.Fatalf("基线应指向远端新版本: %+v", base)
	}
}

// TestSyncUploadLocal 情况二：本地已变、远端等于基线 → 上传本地并更新基线。
func TestSyncUploadLocal(t *testing.T) {
	s, be, _ := newSyncStore(t, "旧内容")
	be.existed, be.remote, be.version = true, docBytes("旧内容"), "r1"
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatalf("首次同步失败: %s", st.Error)
	}
	if err := s.SaveNow(doc("本地新内容")); err != nil {
		t.Fatal(err)
	}
	var gotContent []byte
	var gotExpect string
	be.mu.Lock()
	be.pushFn = func(content []byte, expect string) (string, error) {
		gotContent, gotExpect = append([]byte(nil), content...), expect
		be.remote, be.version, be.existed = append([]byte(nil), content...), "r2", true
		return "r2", nil
	}
	be.mu.Unlock()
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatalf("上传失败: %s", st.Error)
	}
	if !bytes.Equal(gotContent, docBytes("本地新内容")) || gotExpect != "r1" {
		t.Fatalf("上传内容或 CAS 期望错误: content=%q expect=%q", gotContent, gotExpect)
	}
	if base := readBaseline(t, s); base.RemoteVersion != "r2" {
		t.Fatalf("上传后基线应指向新版本: %+v", base)
	}
	if _, v, ok, _, _ := be.snapshot(); !ok || v != "r2" {
		t.Fatalf("远端应更新为新内容: %q", v)
	}
}

func docBytes(text string) []byte {
	raw, _ := encodeForTest(doc(text))
	return raw
}

// TestSyncBothChangedSameContent 情况三：双方都变但内容相同 → 只更新基线。
func TestSyncBothChangedSameContent(t *testing.T) {
	s, be, _ := newSyncStore(t, "同内容")
	be.existed, be.remote, be.version = true, docBytes("同内容"), "r1"
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatal(st.Error)
	}
	// 本地与远端同时改成相同的新内容。
	if err := s.SaveNow(doc("同内容 v2")); err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	be.remote, be.version = docBytes("同内容 v2"), "r2"
	be.mu.Unlock()
	if st := startSync(t, s, be); st.Error != "" || st.Conflict != nil {
		t.Fatalf("内容相同不应冲突: %+v", st)
	}
	if _, _, _, _, pushes := be.snapshot(); pushes != 0 {
		t.Fatalf("只更新基线不应上传: pushes=%d", pushes)
	}
	if base := readBaseline(t, s); base.ContentSHA256 != contentHash(docBytes("同内容 v2")) || base.RemoteVersion != "r2" {
		t.Fatalf("基线应刷新: %+v", base)
	}
}

// TestSyncConflictResolve 情况四：双向分叉 → 冲突挂起；二选一后落选方
// 留档 conflicts/zakki-YYYYMMDD-HHmmss-<设备名>.json。
func TestSyncConflictResolve(t *testing.T) {
	for _, keepLocal := range []bool{true, false} {
		t.Run(map[bool]string{true: "保留本机", false: "保留远端"}[keepLocal], func(t *testing.T) {
			s, be, _ := newSyncStore(t, "基线")
			be.existed, be.remote, be.version = true, docBytes("基线"), "r1"
			if st := startSync(t, s, be); st.Error != "" {
				t.Fatal(st.Error)
			}
			if err := s.SaveNow(doc("本机修改")); err != nil {
				t.Fatal(err)
			}
			be.mu.Lock()
			be.remote, be.version = docBytes("远端修改"), "r2"
			be.mu.Unlock()
			if st := startSync(t, s, be); st.Conflict == nil {
				t.Fatalf("双向分叉应挂起冲突: %+v", st)
			}
			// 冲突期间不再重复进入判定（同一轮已经挂起）。
			if _, _, _, _, pushes := be.snapshot(); pushes != 0 {
				t.Fatal("冲突轮不应上传")
			}
			s.ResolveSyncConflict(keepLocal)
			waitSyncIdle(t, s)
			st := s.SyncStateSnapshot()
			if st.Conflict != nil || st.Error != "" {
				t.Fatalf("裁决后冲突应解除: %+v", st)
			}
			// 留档：命名 zakki-YYYYMMDD-HHmmss-<设备名>.json，内容为落选方。
			entries, err := os.ReadDir(s.ConflictsDir())
			if err != nil || len(entries) != 1 {
				t.Fatalf("conflicts/ 应有一个留档: %v", err)
			}
			nameRe := regexp.MustCompile(`^zakki-\d{8}-\d{6}-[^/]+\.json$`)
			if !nameRe.MatchString(entries[0].Name()) {
				t.Fatalf("留档命名不符: %q", entries[0].Name())
			}
			archived, err := os.ReadFile(filepath.Join(s.ConflictsDir(), entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			wantLoser := docBytes("远端修改")
			if !keepLocal {
				wantLoser = docBytes("本机修改")
			}
			if !bytes.Equal(archived, wantLoser) {
				t.Fatalf("留档应为落选版本: got %q want %q", archived, wantLoser)
			}
			// 胜出版本落盘且基线一致。
			got, _ := os.ReadFile(s.DataPath())
			wantWinner := docBytes("本机修改")
			if !keepLocal {
				wantWinner = docBytes("远端修改")
			}
			if !bytes.Equal(got, wantWinner) {
				t.Fatalf("胜出版本应落盘: got %q want %q", got, wantWinner)
			}
			if keepLocal && s.SyncNeedsReload() {
				t.Fatal("保留本机=上传远端，本地内容不变，不应请求重载")
			}
			if !keepLocal && !s.SyncNeedsReload() {
				t.Fatal("保留远端=远端落盘，应请求重载")
			}
		})
	}
}

// TestSyncCASConflictRepull CAS 冲突：上传被拒后重新 pull 重判，
// 不盲重试旧版本号。
func TestSyncCASConflictRepull(t *testing.T) {
	t.Run("远端漂移到同内容新版本：重判后带最新版本号上传", func(t *testing.T) {
		s, be, _ := newSyncStore(t, "基线")
		be.existed, be.remote, be.version = true, docBytes("基线"), "r1"
		if st := startSync(t, s, be); st.Error != "" {
			t.Fatal(st.Error)
		}
		if err := s.SaveNow(doc("本地新")); err != nil {
			t.Fatal(err)
		}
		be.mu.Lock()
		first := true
		be.pushFn = func(content []byte, expect string) (string, error) {
			if first {
				// 上传途中其他设备以新版本号重推了相同内容：CAS 拒绝。
				first = false
				if expect != "r1" {
					t.Errorf("首轮应带基线版本号, got %q", expect)
				}
				be.remote, be.version = docBytes("基线"), "r2"
				return "", ErrSyncConflict
			}
			if expect != "r2" {
				t.Errorf("重判后应带刚 pull 到的最新版本号, got %q", expect)
			}
			if !bytes.Equal(content, docBytes("本地新")) {
				t.Errorf("重判后应上传本地内容, got %q", content)
			}
			be.remote, be.version, be.existed = append([]byte(nil), content...), "r3", true
			return "r3", nil
		}
		be.mu.Unlock()
		if st := startSync(t, s, be); st.Error != "" {
			t.Fatalf("CAS 重判失败: %s", st.Error)
		}
		if base := readBaseline(t, s); base.RemoteVersion != "r3" {
			t.Fatalf("基线应指向 r3: %+v", base)
		}
	})
	t.Run("远端变成不同内容：重判落入冲突", func(t *testing.T) {
		s, be, _ := newSyncStore(t, "基线")
		be.existed, be.remote, be.version = true, docBytes("基线"), "r1"
		if st := startSync(t, s, be); st.Error != "" {
			t.Fatal(st.Error)
		}
		if err := s.SaveNow(doc("本地新")); err != nil {
			t.Fatal(err)
		}
		be.mu.Lock()
		first := true
		be.pushFn = func([]byte, string) (string, error) {
			if first {
				first = false
				be.remote, be.version = docBytes("别人的内容"), "r2"
				return "", ErrSyncConflict
			}
			t.Error("重判发现双向分叉后不应再次上传")
			return "", ErrSyncConflict
		}
		be.mu.Unlock()
		if st := startSync(t, s, be); st.Conflict == nil {
			t.Fatalf("重判后双向分叉应挂起冲突: %+v", st)
		}
	})
}

// TestSyncCASExhausted 远端持续变化：重判到上限后如实报错，不无限轮。
func TestSyncCASExhausted(t *testing.T) {
	s, be, _ := newSyncStore(t, "基线")
	be.existed, be.remote, be.version = true, docBytes("基线"), "r1"
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatal(st.Error)
	}
	if err := s.SaveNow(doc("本地新")); err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	be.pushFn = func([]byte, string) (string, error) {
		be.version = "r-changed"
		return "", ErrSyncConflict
	}
	be.mu.Unlock()
	st := startSync(t, s, be)
	if st.Error == "" || !strings.Contains(st.Error, "远端持续") {
		t.Fatalf("应报远端持续变化: %+v", st)
	}
	if _, _, _, _, pushes := be.snapshot(); pushes != syncMaxAttempts {
		t.Fatalf("重判次数应恰好 %d: %d", syncMaxAttempts, pushes)
	}
}

// TestSyncFirstSync 空基线：远端缺失直接上传；双方都有内容保守进冲突。
func TestSyncFirstSync(t *testing.T) {
	t.Run("远端缺失上传", func(t *testing.T) {
		s, be, _ := newSyncStore(t, "首次内容")
		if st := startSync(t, s, be); st.Error != "" {
			t.Fatal(st.Error)
		}
		if base := readBaseline(t, s); base.RemoteNil || base.ContentSHA256 == "" {
			t.Fatalf("首同步后基线应建立: %+v", base)
		}
	})
	t.Run("双方都有内容冲突", func(t *testing.T) {
		s, be, _ := newSyncStore(t, "本地已有")
		be.existed, be.remote, be.version = true, docBytes("远端已有"), "r0"
		if st := startSync(t, s, be); st.Conflict == nil {
			t.Fatalf("首同步双向有内容应冲突: %+v", st)
		}
	})
}

// TestSyncFlushesPending 本地待保存先于判定串行落盘（D12）。
func TestSyncFlushesPending(t *testing.T) {
	s, be, _ := newSyncStore(t, "旧")
	be.existed, be.remote, be.version = true, docBytes("旧"), "r1"
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatal(st.Error)
	}
	// 只调度不落盘，同步轮应先把待保存内容写入磁盘再上传。
	s.ScheduleSave(doc("未落盘的新内容"), time.Hour, nil)
	var got []byte
	be.mu.Lock()
	be.pushFn = func(content []byte, _ string) (string, error) {
		got = append([]byte(nil), content...)
		be.remote, be.version, be.existed = append([]byte(nil), content...), "r2", true
		return "r2", nil
	}
	be.mu.Unlock()
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatal(st.Error)
	}
	if !bytes.Equal(got, docBytes("未落盘的新内容")) {
		t.Fatalf("同步应上传 flush 后的磁盘内容: %q", got)
	}
	disk, err := os.ReadFile(s.DataPath())
	if err != nil || !bytes.Equal(disk, docBytes("未落盘的新内容")) {
		t.Fatalf("待保存应先落盘: %q %v", disk, err)
	}
}

// TestSyncSaveBlockedRefuses 写保护（损坏/高版本/外部改动）期间拒绝
// 同步，不覆盖原文件。
func TestSyncSaveBlockedRefuses(t *testing.T) {
	s, be, _ := newSyncStore(t, "原文件")
	s.SetSaveBlocked(true)
	be.existed, be.remote, be.version = true, docBytes("远端"), "r1"
	if st := startSync(t, s, be); !strings.Contains(st.Error, "写保护") {
		t.Fatalf("写保护应拒绝同步: %+v", st)
	}
	got, _ := os.ReadFile(s.DataPath())
	if !bytes.Equal(got, docBytes("原文件")) {
		t.Fatalf("写保护期间不得改写文件: %q", got)
	}
	if _, _, _, _, pushes := be.snapshot(); pushes != 0 {
		t.Fatal("写保护期间不应上传")
	}
}

// TestSyncRemoteMissingAfterBaseline 基线之后远端文件被删：按判定表
// 采用远端（=删除本地），等待界面重载。
func TestSyncRemoteMissingAfterBaseline(t *testing.T) {
	s, be, _ := newSyncStore(t, "本地内容")
	be.existed, be.remote, be.version = true, docBytes("本地内容"), "r1"
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatal(st.Error)
	}
	be.mu.Lock()
	be.existed, be.remote, be.version = false, nil, ""
	be.mu.Unlock()
	if st := startSync(t, s, be); st.Error != "" || !s.SyncNeedsReload() {
		t.Fatalf("远端删除应被采用: %+v", st)
	}
	if _, err := os.Lstat(s.DataPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("本地文件应按远端删除")
	}
	if base := readBaseline(t, s); !base.RemoteNil {
		t.Fatalf("基线应记录远端不存在: %+v", base)
	}
}

// TestSyncConflictArchiveNameCollisions 同秒多次留档追加序号；设备名
// 清洗保证跨平台合法。
func TestSyncConflictArchiveName(t *testing.T) {
	s, _, _ := newSyncStore(t, "x")
	for i := 0; i < 3; i++ {
		if _, err := s.writeConflictArchive([]byte("落选")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(s.ConflictsDir())
	if err != nil || len(entries) != 3 {
		t.Fatalf("应生成 3 个留档: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if len(names) != 3 {
		t.Fatalf("留档命名应唯一: %v", names)
	}
	for _, bad := range []string{"", "a/b", `a\b`, strings.Repeat("长", 100)} {
		if got := sanitizeDeviceName(bad); strings.ContainsAny(got, `/\`) || len(got) > 60 {
			t.Fatalf("设备名清洗失败: %q -> %q", bad, got)
		}
	}
	if got := sanitizeDeviceName("机器/A:B"); got != "机器-A-B" {
		t.Fatalf("非法字符应替换: %q", got)
	}
}

// TestSyncPauseGitScheduler 开启同步暂停旧 Git 调度器；关闭后按保留
// 配置恢复；偏好里的用户开关不被改写。
func TestSyncPauseGitScheduler(t *testing.T) {
	s, be, stateFile := newSyncStore(t, "内容")
	runner := &syncGit{root: filepath.Dir(s.DataPath())}
	s.RunGit = runner.run
	s.ConfigureGit(GitConfig{AutoCommit: true, CommitInterval: time.Millisecond}, nil)
	// 开启同步：短周期内不应有任何 git 调用。
	s.ConfigureSync(be, "gomeen/notes", stateFile, nil)
	s.SyncNow()
	waitSyncIdle(t, s)
	deadline := time.Now().Add(80 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runner.count("rev-parse") > 0 {
			t.Fatal("同步开启期间旧 Git 调度器不应运行")
		}
		time.Sleep(time.Millisecond)
	}
	// 关闭同步：按保留配置恢复（偏好从未被改写，这里是 Store 内存配置）。
	s.ConfigureSync(nil, "", "", nil)
	await(t, func() bool { return runner.count("rev-parse") > 0 })
}

// TestSyncTokenNeverPersists token 只进安全存储：同步全程不写入任何
// 文件，不出现在日志里。
func TestSyncTokenNeverPersists(t *testing.T) {
	s, be, stateFile := newSyncStore(t, "内容")
	const secret = "gho_secret_token_123"
	k := &memKeyring{}
	if err := SaveSyncToken(k, TokenSet{AccessToken: secret}); err != nil {
		t.Fatal(err)
	}
	provider := TokenProvider(k, "client-id", NewDeviceFlow(), nil)
	backend := &capturingBackend{inner: be, provider: provider}
	var logBuf syncBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	be.existed, be.remote, be.version = true, docBytes("内容"), "r1"
	s.ConfigureSync(backend, "gomeen/notes", stateFile, nil)
	if err := s.SaveNow(doc("内容 v2")); err != nil {
		t.Fatal(err)
	}
	s.SyncNow()
	waitSyncIdle(t, s)
	if st := s.SyncStateSnapshot(); st.Error != "" {
		t.Fatalf("同步失败: %s", st.Error)
	}
	// 扫描数据目录所有文件与日志缓冲：不得出现 token。
	root := filepath.Dir(s.DataPath())
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("token 泄漏到文件: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logBuf.Bytes(), []byte(secret)) {
		t.Fatal("token 泄漏到日志")
	}
	if len(backend.authHeaders) == 0 || backend.authHeaders[0] != "Bearer "+secret {
		t.Fatalf("后端应携带 Authorization 头: %v", backend.authHeaders)
	}
}

// capturingBackend 记录后端调用头（模拟 HTTP 层的 Authorization）。
type capturingBackend struct {
	inner       SyncBackend
	provider    func(context.Context) (string, error)
	authHeaders []string
}

func (c *capturingBackend) BackendID() string { return c.inner.BackendID() }

func (c *capturingBackend) Pull(ctx context.Context) ([]byte, string, bool, error) {
	tok, err := c.provider(ctx)
	if err != nil {
		return nil, "", false, err
	}
	c.authHeaders = append(c.authHeaders, "Bearer "+tok)
	return c.inner.Pull(ctx)
}

func (c *capturingBackend) Push(ctx context.Context, content []byte, expect string) (string, error) {
	tok, err := c.provider(ctx)
	if err != nil {
		return "", err
	}
	c.authHeaders = append(c.authHeaders, "Bearer "+tok)
	return c.inner.Push(ctx, content, expect)
}

// memKeyring 是测试用内存安全存储。
type memKeyring struct {
	mu    sync.Mutex
	value []byte
}

func (m *memKeyring) Load() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.value == nil {
		return nil, ErrKeyringEmpty
	}
	return append([]byte(nil), m.value...), nil
}

func (m *memKeyring) Store(v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = append([]byte(nil), v...)
	return nil
}

func (m *memKeyring) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = nil
	return nil
}

// syncBuffer 是线程安全的日志缓冲。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

// TestSyncNotConfiguredNoop 未配置同步时 SyncNow 是 no-op。
func TestSyncNotConfiguredNoop(t *testing.T) {
	s, _, _ := newSyncStore(t, "内容")
	s.SyncNow()
	if s.SyncRunning() {
		t.Fatal("未配置同步不应启动轮次")
	}
	if s.SyncNeedsReload() {
		t.Fatal("未配置同步不应请求重载")
	}
}

// encodeForTest 把 File 编成磁盘字节（与 Store 落盘同一编码管线）。
func encodeForTest(f File) ([]byte, error) {
	return model.Encode(model.Document{File: f})
}
