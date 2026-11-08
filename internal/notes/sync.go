package notes

// 唯一笔记文件的云同步循环（规格 §2.2 / D12 / D13）：
// 每轮先 pull（读取远端内容与版本号）、flush 本地待保存、按 D13 判定表
// 处理，再 commit（CAS 带远端版本号上传）；CAS 失败重新 pull 重判，
// 不盲重试旧版本号。本地落盘与上传串行（D12）：本地阶段持有写锁与
// Git 门闩（与迁址、写保护互斥），网络阶段不占锁。
// 基线是每台设备本地的「上次同步成功」内容 sha256 与后端版本号，存应用
// 数据目录的独立状态文件，不写入 zakki.json、偏好文件或 Git。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"zakki/internal/atomicfile"
)

// ErrSyncConflict 表示远端版本与本轮 pull 时不一致（CAS 拒绝），调用方
// 应重新 pull 并重判，而不是用旧版本号重试。
var ErrSyncConflict = errors.New("远端文件已被其他设备更新")

// ErrSyncHold 表示远端内容已落盘、等待界面重载期间暂停本地保存。重载
// （Load）完成后自动解除；期间新的同步保存会被拒绝而不静默丢弃。
var ErrSyncHold = errors.New("正在应用远端同步内容，请稍后重试")

// syncSaveDebounce 是保存成功后的同步防抖：连续输入时只跟最后一笔。
const syncSaveDebounce = 10 * time.Second

// syncMaxAttempts 是单轮内「pull → 判定 → commit」的最大重判次数；
// 只用于 CAS 冲突重拉，绝不盲重试旧版本号。
const syncMaxAttempts = 3

// SyncBackend 是单一文件同步后端的抽象（任一时刻只激活一个）。version
// 是后端自己的版本号（后端 A 为 blob sha）；content 一律为 zakki.json
// 的完整字节。
type SyncBackend interface {
	// BackendID 对应偏好 sync.backend 的取值。
	BackendID() string
	// Pull 读取远端文件；远端不存在时 existed=false 且 content 为 nil。
	Pull(ctx context.Context) (content []byte, version string, existed bool, err error)
	// Push 以 CAS 语义上传：expect 为本轮 pull 看到的版本号，空串表示远端
	// 应当不存在。远端已被并发修改时返回 ErrSyncConflict。
	Push(ctx context.Context, content []byte, expect string) (newVersion string, err error)
}

// SyncConflict 是 D13 第 4 情况的冲突现场：本地与远端都相对基线变化且
// 内容不同，等待用户二选一。两个内容都是完整文件字节（可为 nil 表示该
// 侧文件不存在）。
type SyncConflict struct {
	LocalContent  []byte
	RemoteContent []byte
	RemoteVersion string
}

// SyncState 发布给 UI 的同步状态快照。
type SyncState struct {
	Enabled bool
	Backend string
	Repo    string
	Running bool
	// LastSync 是最近一次成功完成的轮次时间（含无可做动作的轮）。
	LastSync time.Time
	// LastRemoteApply 是最近一次把远端内容写入本地磁盘的时间；非零表示
	// 界面需要重载磁盘内容（SyncNeedsReload）。
	LastRemoteApply time.Time
	Error           string
	Conflict        *SyncConflict
}

// syncDecision 是 D13 判定表的结论。
type syncDecision int

const (
	syncNone           syncDecision = iota // 双方均未变，无需动作
	syncAdoptRemote                        // 采用远端（以没有未保存本地修改为前提）
	syncUploadLocal                        // 上传本地
	syncUpdateBaseline                     // 内容相同，只更新基线
	syncNeedResolve                        // 双向分叉，挂起冲突等待用户裁决
)

// syncBaselineV1 是基线状态文件的磁盘格式。ContentSHA256 为空表示从未
// 同步；只存内容指纹与后端版本号，绝不包含 token 等凭据（R7）。
type syncBaselineV1 struct {
	Version       int    `json:"version"`
	Backend       string `json:"backend"`
	ContentSHA256 string `json:"contentSha256"`
	RemoteVersion string `json:"remoteVersion"`
	// RemoteNil / LocalNil 区分「内容为空」与「文件不存在」。
	RemoteNil bool   `json:"remoteNil,omitempty"`
	LocalNil  bool   `json:"localNil,omitempty"`
	Device    string `json:"device,omitempty"`    // 写入基线的设备（诊断用）
	UpdatedAt string `json:"updatedAt,omitempty"` // RFC3339（诊断用）
}

// neverSynced 报告基线是否为空：只看 Backend 是否被写入。内容指纹
// 允许为空串（远端文件被删除后采用的基线），不能作为「从未同步」的判据。
func (b syncBaselineV1) neverSynced() bool { return b.Backend == "" }

// syncDirective 是冲突裁决后的定向轮策略：本轮无视判定表，按用户选择处置。
type syncDirective struct {
	keepLocal bool
}

// syncPushRequest 从持锁本地阶段带出的网络上传任务；本地锁已释放后才执行。
type syncPushRequest struct {
	content []byte
	expect  string
	basic   syncBaselineV1 // 上传成功后要写的基线（版本号由 Push 结果补）
}

// ConfigureSync 配置云同步。backend 为 nil 表示关闭：暂停门禁解除，
// git.autoCommit / git.autoPush 按用户保留配置自动恢复（不悄悄改写
// 偏好）。stateFile 是基线状态文件路径（应用数据目录，设备本地，不随
// 笔记文件夹漂移）；after 在每轮结束后回调供 UI 刷新。重复配置同一
// 后端只更新回调，不作废在途轮次。
func (s *Store) ConfigureSync(backend SyncBackend, repo, stateFile string, after func()) {
	s.gitGate.Lock()
	defer s.gitGate.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncAfter = after
	// 同一后端按指针身份判断（实现一律为指针类型）；BackendID 相同的
	// 不同实例（换 token 重连）必须作废在途轮次。
	unchanged := s.syncBackend != nil && backend != nil &&
		s.syncBackend == backend &&
		s.syncRepo == repo && s.syncStateFile == stateFile
	if unchanged {
		return
	}
	s.syncBackend, s.syncRepo, s.syncStateFile = backend, repo, stateFile
	s.syncGen++
	s.stopSyncTimerLocked()
	s.syncQueued = false
	if s.syncCancel != nil {
		s.syncCancel()
		s.syncCancel = nil
	}
	active := backend != nil
	if active == s.syncActive {
		if active {
			// 后端指向变化（换仓库或重连）：清掉旧错误，等下一轮刷新。
			s.syncState.Error = ""
			s.syncState.Backend, s.syncState.Repo = backend.BackendID(), repo
		}
		return
	}
	s.syncActive = active
	if active {
		s.syncState.Enabled, s.syncState.Backend, s.syncState.Repo = true, backend.BackendID(), repo
		s.invalidateGitLocked(false) // 开启同步：暂停旧 Git 调度器（§2.2）
	} else {
		s.syncState = SyncState{}
		s.armGitLocked() // 关闭同步：按保留配置恢复 Git 备份
	}
}

// SyncNow 请求一轮同步（异步）；未配置或已停止时为 no-op，运行中记排队。
func (s *Store) SyncNow() {
	s.mu.Lock()
	if s.syncBackend == nil || s.closed || s.stopped {
		s.mu.Unlock()
		return
	}
	if s.syncRunning {
		s.syncQueued = true
		s.mu.Unlock()
		return
	}
	s.syncRunning = true
	s.syncDone = make(chan struct{})
	gen := s.syncGen + 1
	s.syncGen = gen
	s.syncState.Running = true
	s.mu.Unlock()
	go func() {
		s.syncRun(gen)
		s.mu.Lock()
		s.syncRunning = false
		close(s.syncDone)
		again := s.syncQueued && s.syncBackend != nil && !s.closed && !s.stopped
		s.syncQueued = false
		s.mu.Unlock()
		if again {
			s.SyncNow()
		}
	}()
}

// SyncRunning 报告是否有同步轮在途。
func (s *Store) SyncRunning() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.syncRunning }

// WaitSyncIdle 等待在途同步轮结束（不含将来的排程）。
func (s *Store) WaitSyncIdle() {
	for {
		s.mu.Lock()
		if !s.syncRunning {
			s.mu.Unlock()
			return
		}
		done := s.syncDone
		s.mu.Unlock()
		<-done
	}
}

// SyncStateSnapshot 返回同步状态副本（主线程读取）。
func (s *Store) SyncStateSnapshot() SyncState { s.mu.Lock(); defer s.mu.Unlock(); return s.syncState }

// SyncNeedsReload 报告远端内容已落盘、界面尚未重载。
func (s *Store) SyncNeedsReload() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.syncHold }

// ConflictsDir 返回冲突留档目录（与 zakki.json 同目录的 conflicts/）。
// 留档文件不进标签、不参与任何调度、不计标签上限；旧 Git 调度器只
// add/commit zakki.json 的精确路径，留档与基线都不会被 Git 备份收录。
func (s *Store) ConflictsDir() string {
	return filepath.Join(filepath.Dir(s.DataPath()), "conflicts")
}

// ResolveSyncConflict 按 D13 处置挂起的冲突：keepLocal=true 保留本机
// （远端版本留档后上传本地），否则采用远端（本地版本留档后落盘重载）。
// 留档失败不推进裁决：保留冲突现场并如实上报，用户可重试。
func (s *Store) ResolveSyncConflict(keepLocal bool) {
	s.mu.Lock()
	cf := s.syncState.Conflict
	configured := s.syncBackend != nil && !s.closed
	s.mu.Unlock()
	if cf == nil || !configured {
		return
	}
	loser := cf.LocalContent
	if keepLocal {
		loser = cf.RemoteContent
	}
	if _, err := s.writeConflictArchive(loser); err != nil {
		s.mu.Lock()
		s.syncState.Error = "冲突留档失败：" + err.Error()
		s.mu.Unlock()
		s.notifySyncAfter()
		return
	}
	s.mu.Lock()
	s.syncState.Conflict = nil
	s.syncState.Error = ""
	s.syncDirective = &syncDirective{keepLocal: keepLocal}
	if s.syncRunning { // 已有轮在途：让排队机制接手，保留定向策略
		s.syncQueued = true
		s.mu.Unlock()
		return
	}
	s.syncRunning = true
	s.syncDone = make(chan struct{})
	gen := s.syncGen + 1
	s.syncGen = gen
	s.syncState.Running = true
	s.mu.Unlock()
	go func() {
		s.syncRun(gen)
		s.mu.Lock()
		s.syncRunning = false
		close(s.syncDone)
		again := s.syncQueued && s.syncBackend != nil && !s.closed && !s.stopped
		s.syncQueued = false
		s.mu.Unlock()
		if again {
			s.SyncNow()
		}
	}()
}

// notifySyncAfter 在轮次结束后回调 UI（不占任何锁）。
func (s *Store) notifySyncAfter() {
	s.mu.Lock()
	after := s.syncAfter
	s.mu.Unlock()
	if after != nil {
		after()
	}
}

func (s *Store) syncValidLocked(gen uint64) bool {
	return gen == s.syncGen && !s.closed && !s.stopped && s.syncBackend != nil
}

// syncRun 执行一轮同步：pull → 本地判定与落盘 → CAS commit。CAS 冲突
// 重新 pull 重判（上限 syncMaxAttempts），不盲重试旧版本号。
func (s *Store) syncRun(gen uint64) {
	var lastErr error
	lastWasRetry := false
	for attempt := 0; attempt < syncMaxAttempts; attempt++ {
		s.mu.Lock()
		backend := s.syncBackend
		valid := s.syncValidLocked(gen)
		s.mu.Unlock()
		if !valid {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		if attempt == 0 {
			s.mu.Lock()
			s.syncCancel = cancel
			s.mu.Unlock()
		}
		remote, version, existed, err := backend.Pull(ctx)
		cancel()
		if err != nil {
			lastErr = err
			break
		}
		s.mu.Lock()
		valid = s.syncValidLocked(gen)
		s.mu.Unlock()
		if !valid {
			return
		}
		retry, err := s.syncRound(gen, backend, remote, version, existed, ctx)
		if err != nil {
			lastErr = err
			break
		}
		if !retry {
			// 本轮已收敛（完成或挂起冲突）：清除重判标记。
			lastErr = nil
			lastWasRetry = false
			break
		}
		// CAS 冲突：进入下一轮重新 pull 重判。
		lastWasRetry = true
		lastErr = nil
	}
	if lastErr == nil && lastWasRetry {
		lastErr = errors.New("远端持续被其他设备更新，已完成重判仍未提交；请稍后重试")
	}
	s.mu.Lock()
	if gen == s.syncGen {
		st := &s.syncState
		st.Running = false
		st.Enabled = true
		if s.syncBackend != nil {
			st.Backend, st.Repo = s.syncBackend.BackendID(), s.syncRepo
		}
		if lastErr == nil {
			st.Error = ""
			st.LastSync = s.now()
		} else {
			st.Error = lastErr.Error()
		}
	}
	s.mu.Unlock()
	s.notifySyncAfter()
}

// syncRound 编排单次「pull 后的本地阶段 + commit」：持锁完成 flush、
// 判定与本地落盘，网络上传在不持锁状态下进行（D12：本地落盘与上传
// 串行，但网络不阻塞新的本地保存）。retry=true 表示 CAS 冲突需重拉。
func (s *Store) syncRound(gen uint64, backend SyncBackend, remote []byte, version string, existed bool, ctx context.Context) (retry bool, err error) {
	s.writeMu.Lock()
	s.gitGate.Lock()
	push, retry, err := s.syncLocalPhase(gen, backend, remote, version, existed)
	s.gitGate.Unlock()
	s.writeMu.Unlock()
	if err != nil || retry || push == nil {
		return retry, err
	}
	newVersion, err := backend.Push(ctx, push.content, push.expect)
	if err != nil {
		if errors.Is(err, ErrSyncConflict) {
			return true, nil
		}
		return false, err
	}
	// 上传成功：远端必然存在，基线补上新的版本号。
	push.basic.RemoteVersion = newVersion
	push.basic.RemoteNil = false
	s.writeMu.Lock()
	err = s.saveSyncBaseline(push.basic)
	s.writeMu.Unlock()
	return false, err
}

// syncLocalPhase 是判定表的持锁实现（写锁顺序沿用 SetSaveBlocked：
// writeMu → gitGate → mu）。返回待执行的上传任务（如有）。
func (s *Store) syncLocalPhase(gen uint64, backend SyncBackend, remote []byte, version string, existed bool) (push *syncPushRequest, retry bool, err error) {
	s.mu.Lock()
	valid := s.syncValidLocked(gen)
	blocked := s.saveBlocked || s.invalid
	hold := s.syncHold
	s.mu.Unlock()
	if !valid {
		return nil, false, nil
	}
	if hold {
		// 上一轮的远端内容还没被界面重载：不再叠加动作，等 UI 消化。
		return nil, false, nil
	}
	if blocked {
		// 写保护（损坏/高版本/外部改动/用户暂停）：绝不覆盖原文件。
		return nil, false, errors.New("笔记文件处于写保护状态，同步已暂停；请先处理文件问题或重新读取")
	}
	// 处理本地待保存（D12）：先把内存中的未保存快照串行落盘，
	// 让磁盘内容成为「本地状态」的唯一事实。
	if err := s.flushPendingNow(); err != nil {
		return nil, false, fmt.Errorf("同步前保存本地修改失败：%w", err)
	}
	// 核对磁盘指纹（与 Git 提交同一门禁）：文件被外部改动、移动或删除
	// 时拒绝同步，不用内存旧内容盲判（R11/R12 语义不回退）。
	if err := s.verifyDisk(); err != nil {
		return nil, false, fmt.Errorf("同步前校验磁盘状态失败：%w", err)
	}
	s.mu.Lock()
	rev0 := s.saveCompleted
	s.mu.Unlock()
	base, err := s.loadSyncBaseline()
	if err != nil {
		return nil, false, err
	}
	directive := s.takeSyncDirective()

	s.mu.Lock()
	local, localExisted := s.savedContent, s.diskExists
	local = append([]byte(nil), local...)
	s.mu.Unlock()
	localHash := contentHash(local)
	remoteHash := contentHash(remote)
	decision := decideSync(localExisted, localHash, existed, remoteHash, base, directive)

	switch decision {
	case syncNone:
		return nil, false, nil
	case syncUpdateBaseline:
		return nil, false, s.saveSyncBaseline(s.baselineUpdated(base, backend, remoteHash, version, existed, localExisted))
	case syncAdoptRemote:
		return nil, false, s.syncAdopt(remote, version, existed, rev0)
	case syncNeedResolve:
		s.mu.Lock()
		s.syncState.Conflict = &SyncConflict{
			LocalContent:  local,
			RemoteContent: append([]byte(nil), remote...),
			RemoteVersion: version,
		}
		s.mu.Unlock()
		return nil, false, nil
	case syncUploadLocal:
		if !localExisted {
			return nil, false, errors.New("本地笔记文件不存在，无法上传；请重新读取或检查保存位置")
		}
		// 上传的 CAS 期望用本轮刚 pull 到的版本号：能走到上传分支，说明
		// 远端内容与基线一致（或按裁决允许覆盖）；用最新版本号可避免
		// 「内容相同但版本号漂移」造成的无意义 CAS 失败；远端缺失时期望
		// 为空（远端应当不存在）。
		expect := ""
		if directive != nil && directive.keepLocal || existed {
			expect = version
		}
		return &syncPushRequest{
			content: local,
			expect:  expect,
			basic:   s.baselineUpdated(base, backend, localHash, "", existed, localExisted),
		}, false, nil
	}
	return nil, false, nil
}

// baselineUpdated 生成动作完成后的基线（版本号在 upload 成功后补）。
func (s *Store) baselineUpdated(base syncBaselineV1, backend SyncBackend, hash, version string, remoteExisted, localExisted bool) syncBaselineV1 {
	id := base.Backend
	if backend != nil {
		id = backend.BackendID()
	}
	return syncBaselineV1{
		Version: 1, Backend: id, ContentSHA256: hash, RemoteVersion: version,
		RemoteNil: !remoteExisted, LocalNil: !localExisted,
		Device: deviceName(), UpdatedAt: s.now().Format(time.RFC3339),
	}
}

// syncAdopt 把远端内容写入本地磁盘（校验通过才跟随，R8）：verifyDisk →
// 原子替换 → 更新磁盘指纹 → 基线 → 冻结未重载期间的保存并请求界面重载。
func (s *Store) syncAdopt(remote []byte, version string, remoteExisted bool, rev0 uint64) error {
	if err := s.verifyDisk(); err != nil {
		// 磁盘与记录指纹不符（外部改动/移动/删除）：不安全跟随。
		return fmt.Errorf("远端内容未能安全落盘：%w", err)
	}
	s.mu.Lock()
	// 判定与写入之间又出现新的本地快照：中止本轮（不丢输入），下一轮按
	// 「本地已变、远端未变」走上传路径。
	if s.pending != nil || s.saveRevision != rev0 {
		s.mu.Unlock()
		return errors.New("同步期间出现新的本地修改，本轮已跳过，稍后自动重试")
	}
	s.mu.Unlock()
	if remoteExisted {
		if err := s.stateWrite(s.dataPath, remote); err != nil && !atomicfile.IsCommitted(err) {
			return fmt.Errorf("写入远端内容失败：%w", err)
		}
	} else if err := s.removeLocalForSync(); err != nil {
		return err
	}
	s.mu.Lock()
	s.diskKnown, s.diskExists = true, remoteExisted
	if remoteExisted {
		s.diskHash = sha256.Sum256(remote)
		s.savedContent = append([]byte(nil), remote...)
	} else {
		s.diskHash = [32]byte{}
		s.savedContent = nil
	}
	// 已接受但未写入的请求全部过期：内存状态由界面重载磁盘内容接管。
	s.resetLoadedSaveLocked()
	s.syncHold = true
	s.syncState.LastRemoteApply = s.now()
	s.mu.Unlock()
	return s.saveSyncBaseline(syncBaselineV1{
		Version: 1, Backend: s.backendIDForBase(), ContentSHA256: contentHash(remote),
		RemoteVersion: version, RemoteNil: !remoteExisted, LocalNil: false,
		Device: deviceName(), UpdatedAt: s.now().Format(time.RFC3339),
	})
}

// removeLocalForSync 按远端「无文件」删除本地文件（采用远端路径）。
func (s *Store) removeLocalForSync() error {
	if err := s.stateRemove(s.dataPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("按远端删除本地文件失败：%w", err)
	}
	return nil
}

// takeSyncDirective 取走挂起的裁决策略（一次性）。
func (s *Store) takeSyncDirective() *syncDirective {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.syncDirective
	s.syncDirective = nil
	return d
}

func (s *Store) backendIDForBase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncBackend == nil {
		return ""
	}
	return s.syncBackend.BackendID()
}

// decideSync 是 D13 判定表的纯函数（「已变」指与本地基线比较；冲突按
// 整个文件处理）。空基线（从未同步）时：本地有文件即本地已变，远端
// 存在即远端已变——全新空仓库直接上传，双方都有内容则保守进冲突。
func decideSync(
	localExisted bool, localHash string,
	remoteExisted bool, remoteHash string,
	base syncBaselineV1,
	directive *syncDirective,
) syncDecision {
	if directive != nil {
		if directive.keepLocal {
			return syncUploadLocal
		}
		return syncAdoptRemote
	}
	var localChanged, remoteChanged bool
	if base.neverSynced() {
		localChanged = localExisted
		remoteChanged = remoteExisted
	} else {
		baseLocal := !base.LocalNil
		baseRemote := !base.RemoteNil
		localChanged = localExisted != baseLocal || (localExisted && localHash != base.ContentSHA256)
		remoteChanged = remoteExisted != baseRemote || (remoteExisted && remoteHash != base.ContentSHA256)
	}
	// 本地文件被删除（相对基线）而远端未变：按远端恢复，而不是把缺失
	// 当作「空内容」上传去清空远端。
	if localChanged && !remoteChanged && !localExisted {
		return syncAdoptRemote
	}
	switch {
	case !localChanged && !remoteChanged:
		return syncNone
	case !localChanged && remoteChanged:
		return syncAdoptRemote
	case localChanged && !remoteChanged:
		return syncUploadLocal
	case localChanged && remoteChanged:
		if localExisted == remoteExisted && localHash == remoteHash {
			return syncUpdateBaseline
		}
		return syncNeedResolve
	}
	return syncNone
}

// loadSyncBaseline 读取基线状态文件；缺失按「从未同步」处理；损坏按
// 「从未同步」降级并记录日志——损坏基线只会让判定走向保守的冲突路径，
// 不会静默覆盖任何一侧。
func (s *Store) loadSyncBaseline() (syncBaselineV1, error) {
	s.mu.Lock()
	path := s.syncStateFile
	s.mu.Unlock()
	if path == "" {
		return syncBaselineV1{}, errors.New("缺少同步基线状态文件路径（配置不完整）")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return syncBaselineV1{Version: 1}, nil
	}
	if err != nil {
		return syncBaselineV1{}, fmt.Errorf("读取同步基线失败：%w", err)
	}
	var base syncBaselineV1
	if err := json.Unmarshal(raw, &base); err != nil {
		slog.Warn("同步基线文件损坏，按从未同步处理", "path", path, "error", err)
		return syncBaselineV1{Version: 1}, nil
	}
	return base, nil
}

// saveSyncBaseline 原子写基线状态文件（应用数据目录，设备本地）。
func (s *Store) saveSyncBaseline(base syncBaselineV1) error {
	s.mu.Lock()
	path := s.syncStateFile
	s.mu.Unlock()
	if path == "" {
		return errors.New("缺少同步基线状态文件路径（配置不完整）")
	}
	raw, err := json.Marshal(base)
	if err != nil {
		return err
	}
	return s.stateWrite(path, raw)
}

// armSyncAfterSaveLocked 在保存成功后安排防抖同步轮：连续输入只跟
// 最后一笔；未配置、基线冻结（等待重载）或已有排程时不装填。由
// completeSave 在 mu 内调用。
func (s *Store) armSyncAfterSaveLocked() {
	if s.closed || s.stopped || s.syncBackend == nil || s.syncHold || s.syncTimer != nil {
		return
	}
	s.syncTimer = time.AfterFunc(syncSaveDebounce, func() { s.SyncNow() })
}

// stopSyncTimerLocked 取消未触发的防抖同步排程（mu 内调用）。
func (s *Store) stopSyncTimerLocked() {
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		s.syncTimer = nil
	}
}

// flushPendingNow 立即把当前待保存快照写盘（不推迟），供同步轮读取
// 确定的本地状态。无待保存时为 no-op。
func (s *Store) flushPendingNow() error {
	s.mu.Lock()
	req := s.pending
	s.pending = nil
	s.stopTimerLocked()
	s.mu.Unlock()
	if req == nil {
		return nil
	}
	return s.saveRequest(req)
}

// writeConflictArchive 把落选版本完整写入 conflicts/（D13 命名：
// zakki-YYYYMMDD-HHmmss-<设备名>.json，与 zakki.json 同目录），同秒冲突
// 追加序号。留档不进标签、不参与调度、不计标签上限，也不自动清理。
func (s *Store) writeConflictArchive(content []byte) (string, error) {
	if content == nil {
		return "", errors.New("留档内容为空")
	}
	dir := s.ConflictsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stem := fmt.Sprintf("zakki-%s-%s", s.now().Format("20060102-150405"), sanitizeDeviceName(deviceName()))
	path := filepath.Join(dir, stem+".json")
	for i := 2; ; i++ {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return "", err
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.json", stem, i))
	}
	if err := s.stateWrite(path, content); err != nil {
		return "", err
	}
	return path, nil
}

// contentHash 返回内容的 sha256 hex；nil 表示无文件，返回空串。
func contentHash(content []byte) string {
	if content == nil {
		return ""
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ConflictInfo 是 conflicts/ 留档的列表条目（设置页查看与删除用）。
type ConflictInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// ListConflictArchives 枚举留档目录中的 JSON 留档；目录不存在返回空
// 列表（不报错）。条目按修改时间倒序（新的在前）。
func ListConflictArchives(dir string) []ConflictInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]ConflictInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, ConflictInfo{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

// deviceName 返回本机名（留档与基线诊断用）；失败回退 unknown。
func deviceName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "unknown"
	}
	return strings.TrimSpace(name)
}

// sanitizeDeviceName 把设备名变成跨平台安全的文件名片段：替换路径分隔符
// 与控制字符，限制长度；空结果回退 unknown。
func sanitizeDeviceName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			b.WriteRune('-')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.Trim(strings.TrimSpace(b.String()), "-.")
	if out == "" {
		return "unknown"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}
