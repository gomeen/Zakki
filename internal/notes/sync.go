package notes

// 唯一笔记文件的云同步循环（规格 §2.2 / D12 / ADR-0004）：
// 每轮先 pull（读取远端内容与版本号）、flush 本地待保存、按判定表处理，
// 再 commit（CAS 带远端版本号上传）；CAS 失败重新 pull 重判，不盲重试
// 旧版本号。本地落盘与上传串行（D12）：本地阶段持有写锁与 Git 门闩
// （与迁址、写保护互斥），网络阶段不占锁。
// 双方都改时不弹窗、不留档：以基线原文逐标签三方合并，同一标签两边都改
// 按较新者胜（sync_merge.go）；从未同步过的设备以云端为准。
// 基线是每台设备本地的「上次同步成功」内容（指纹、原文）与后端版本号，存
// 应用数据目录的独立状态文件，不写入 zakki.json、偏好文件或 Git。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
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
// 改动要尽快离开本机，换设备时另一端才拉得到（ADR-0004）。
// var 以便测试注入短周期（notes 包测试串行执行）。
var syncSaveDebounce = 3 * time.Second

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

// SyncModTimer 由能报告远端文件最近修改时间的后端实现；只在双方都改了
// 同一处、需要「较新者胜」时调用一次。未实现的后端按远端时间未知处理
// （本机胜出）。
type SyncModTimer interface {
	RemoteModTime(ctx context.Context) (time.Time, error)
}

// SyncState 发布给 UI 的同步状态快照。
type SyncState struct {
	Enabled bool
	Backend string
	Repo    string
	Running bool
	// LastSync 是最近一次成功完成的轮次时间（含无可做动作的轮）。
	LastSync time.Time
	// LastRemoteApply 是最近一次把远端（或合并）内容写入本地磁盘的时间；
	// 非零表示界面需要重载磁盘内容（SyncNeedsReload）。
	LastRemoteApply time.Time
	// LastMerge 是最近一次合并两端修改的时间；与 LastRemoteApply 相同表示
	// 这次写入本地的是合并结果。
	LastMerge time.Time
	Error     string
}

// syncDecision 是 D13 判定表的结论。
type syncDecision int

func (d syncDecision) String() string {
	switch d {
	case syncNone:
		return "none"
	case syncAdoptRemote:
		return "adopt-remote"
	case syncUploadLocal:
		return "upload-local"
	case syncUpdateBaseline:
		return "update-baseline"
	case syncMerge:
		return "merge"
	}
	return fmt.Sprintf("unknown(%d)", int(d))
}

const (
	syncNone           syncDecision = iota // 双方均未变，无需动作
	syncAdoptRemote                        // 采用远端（以没有未保存本地修改为前提）
	syncUploadLocal                        // 上传本地
	syncUpdateBaseline                     // 内容相同，只更新基线
	syncMerge                              // 双方都改且内容不同：逐标签合并，同处较新者胜
)

// syncBaselineV1 是基线状态文件的磁盘格式。Backend 为空表示从未同步；
// Content 是上次同步成功时的完整原文（合并的公共祖先，旧基线可能没有）。
// 绝不包含 token 等凭据（R7）。
type syncBaselineV1 struct {
	Version       int    `json:"version"`
	Backend       string `json:"backend"`
	ContentSHA256 string `json:"contentSha256"`
	RemoteVersion string `json:"remoteVersion"`
	// RemoteNil / LocalNil 区分「内容为空」与「文件不存在」。
	RemoteNil bool   `json:"remoteNil,omitempty"`
	LocalNil  bool   `json:"localNil,omitempty"`
	Content   []byte `json:"content,omitempty"`
	Device    string `json:"device,omitempty"`    // 写入基线的设备（诊断用）
	UpdatedAt string `json:"updatedAt,omitempty"` // RFC3339（诊断用）
}

// neverSynced 报告基线是否为空：只看 Backend 是否被写入。内容指纹
// 允许为空串（远端文件被删除后采用的基线），不能作为「从未同步」的判据。
func (b syncBaselineV1) neverSynced() bool { return b.Backend == "" }

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
		s.syncRun(context.Background(), gen)
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

// SyncHasLocalChanges 报告本机是否有尚未推送的修改迹象（待保存快照，或
// 保存后的防抖同步还没触发）。只看内存状态，不碰磁盘与网络，供窗口失焦
// 时决定要不要立即推送。
func (s *Store) SyncHasLocalChanges() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncBackend != nil && (s.pending != nil || s.syncTimer != nil)
}

// FinalSync 在退出最终保存（Shutdown）之后、Close 之前尽力推送一轮，最多
// 等 timeout。本机内容与基线一致时直接返回，不碰网络；超时或失败只返回
// 错误，不阻止退出——下次启动按「本机已变、远端未变」自动补推。
func (s *Store) FinalSync(timeout time.Duration) error {
	s.mu.Lock()
	if s.syncBackend == nil || s.closed || s.syncHold || !s.diskKnown {
		s.mu.Unlock()
		return nil
	}
	localHash, localExisted := contentHash(s.savedContent), s.diskExists
	// 作废在途轮次并中止它的网络请求，最终轮不排在它后面等 HTTP 超时。
	s.syncGen++
	s.syncQueued = false
	s.stopSyncTimerLocked()
	if s.syncCancel != nil {
		s.syncCancel()
		s.syncCancel = nil
	}
	s.mu.Unlock()
	base, err := s.loadSyncBaseline()
	if err != nil {
		return err
	}
	if !base.neverSynced() && localExisted == !base.LocalNil && localHash == base.ContentSHA256 {
		return nil
	}
	s.WaitSyncIdle()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.mu.Lock()
	if s.syncBackend == nil || s.closed || s.syncRunning {
		s.mu.Unlock()
		return nil
	}
	s.syncRunning, s.syncFinal = true, true
	s.syncDone = make(chan struct{})
	gen := s.syncGen + 1
	s.syncGen = gen
	s.mu.Unlock()
	s.syncRun(ctx, gen)
	s.mu.Lock()
	s.syncRunning, s.syncFinal = false, false
	close(s.syncDone)
	syncErr := s.syncState.Error
	s.mu.Unlock()
	if syncErr != "" {
		return errors.New(syncErr)
	}
	return nil
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

// notifySyncAfter 在轮次结束后回调 UI（不占任何锁）。
func (s *Store) notifySyncAfter() {
	s.mu.Lock()
	after := s.syncAfter
	s.mu.Unlock()
	if after != nil {
		after()
	}
}

// syncValidLocked 校验轮次仍有效；FinalSync 的最终轮在 Shutdown 之后运行，
// 不受 stopped 约束。
func (s *Store) syncValidLocked(gen uint64) bool {
	return gen == s.syncGen && !s.closed && (!s.stopped || s.syncFinal) && s.syncBackend != nil
}

// syncRun 执行一轮同步：pull → 本地判定与落盘 → CAS commit。CAS 冲突
// 重新 pull 重判（上限 syncMaxAttempts），不盲重试旧版本号。parent 的
// 取消或超时会中止在途网络请求。
func (s *Store) syncRun(parent context.Context, gen uint64) {
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
		ctx, cancel := context.WithCancel(parent)
		if attempt == 0 {
			s.mu.Lock()
			s.syncCancel = cancel
			s.mu.Unlock()
		}
		remote, version, existed, err := backend.Pull(ctx)
		if err != nil {
			cancel()
			lastErr = err
			break
		}
		s.mu.Lock()
		valid = s.syncValidLocked(gen)
		s.mu.Unlock()
		if !valid {
			cancel()
			return
		}
		retry, err := s.syncRound(gen, backend, remote, version, existed, ctx)
		// pull 与 commit 共用同一轮上下文，本地阶段结束后才取消；
		// Close 经 syncCancel 中止在途网络请求。
		cancel()
		if err != nil {
			lastErr = err
			break
		}
		if !retry {
			// 本轮已收敛：清除重判标记。
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
	if lastErr != nil {
		// 离线时在途请求等 HTTP 客户端超时（60s）才落地；失败原因进日志
		// 与状态栏，恢复后由下一次激活/保存防抖重新触发（无自动重试定时器）。
		slog.Warn("同步轮失败", "error", lastErr)
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
// 判定与本地落盘，网络（上传、查远端修改时间）在不持锁状态下进行（D12：
// 本地落盘与上传串行，但网络不阻塞新的本地保存）。retry=true 表示 CAS 冲突需重拉。
func (s *Store) syncRound(gen uint64, backend SyncBackend, remote []byte, version string, existed bool, ctx context.Context) (retry bool, err error) {
	s.writeMu.Lock()
	s.gitGate.Lock()
	plan, err := s.syncLocalPhase(gen, backend, remote, version, existed)
	s.gitGate.Unlock()
	s.writeMu.Unlock()
	if err != nil || plan == nil {
		return false, err
	}
	push := plan.push
	if plan.merge != nil {
		push, err = s.syncMergeRound(ctx, gen, backend, plan.merge)
		if err != nil || push == nil {
			return false, err
		}
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
// writeMu → gitGate → mu）。
func (s *Store) syncLocalPhase(gen uint64, backend SyncBackend, remote []byte, version string, existed bool) (*syncPlan, error) {
	s.mu.Lock()
	valid := s.syncValidLocked(gen)
	blocked := s.saveBlocked || s.invalid
	hold := s.syncHold
	s.mu.Unlock()
	if !valid {
		return nil, nil
	}
	if hold {
		// 上一轮写入本地的内容还没被界面重载：不再叠加动作，等 UI 消化。
		return nil, nil
	}
	if blocked {
		// 写保护（损坏/高版本/外部改动/用户暂停）：绝不覆盖原文件。
		return nil, errors.New("笔记文件处于写保护状态，同步已暂停；请先处理文件问题或重新读取")
	}
	// 处理本地待保存（D12）：先把内存中的未保存快照串行落盘，
	// 让磁盘内容成为「本地状态」的唯一事实。
	if err := s.flushPendingNow(); err != nil {
		return nil, fmt.Errorf("同步前保存本地修改失败：%w", err)
	}
	// 核对磁盘指纹（与 Git 提交同一门禁）：文件被外部改动、移动或删除
	// 时拒绝同步，不用内存旧内容盲判（R11/R12 语义不回退）。
	if err := s.verifyDisk(); err != nil {
		return nil, fmt.Errorf("同步前校验磁盘状态失败：%w", err)
	}
	s.mu.Lock()
	rev0 := s.saveCompleted
	s.mu.Unlock()
	base, err := s.loadSyncBaseline()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	local, localExisted := s.savedContent, s.diskExists
	local = append([]byte(nil), local...)
	s.mu.Unlock()
	localHash := contentHash(local)
	remoteHash := contentHash(remote)
	decision := decideSync(localExisted, localHash, existed, remoteHash, base)
	// 判定与三方指纹进日志：双实例排查时能对出「哪一端、哪一轮、按什么
	// 判据」落的动作。
	slog.Info("同步轮判定",
		"decision", decision,
		"local", contentHashPrefix(localHash),
		"remote", contentHashPrefix(remoteHash),
		"base", contentHashPrefix(base.ContentSHA256))

	switch decision {
	case syncNone:
		return nil, nil
	case syncUpdateBaseline:
		return nil, s.saveSyncBaseline(s.baselineUpdated(backend, remote, version, existed, localExisted))
	case syncAdoptRemote:
		return nil, s.syncAdopt(remote, version, existed, rev0, false)
	case syncMerge:
		var modAt time.Time
		if info, err := os.Stat(s.DataPath()); err == nil {
			modAt = info.ModTime()
		}
		return &syncPlan{merge: &syncMergeInput{
			base: base, local: local, remote: append([]byte(nil), remote...),
			version: version, rev0: rev0, localModifiedAt: modAt,
		}}, nil
	case syncUploadLocal:
		if !localExisted {
			return nil, errors.New("本地笔记文件不存在，无法上传；请重新读取或检查保存位置")
		}
		// 上传的 CAS 期望用本轮刚 pull 到的版本号：能走到上传分支，说明
		// 远端内容与基线一致；用最新版本号可避免「内容相同但版本号漂移」
		// 造成的无意义 CAS 失败；远端缺失时期望为空（远端应当不存在）。
		expect := ""
		if existed {
			expect = version
		}
		return &syncPlan{push: &syncPushRequest{
			content: local,
			expect:  expect,
			basic:   s.baselineUpdated(backend, local, "", existed, localExisted),
		}}, nil
	}
	return nil, nil
}

// baselineUpdated 生成动作完成后的基线（版本号在 upload 成功后补）。content 是
// 双方此刻一致的内容，作为下次合并的公共祖先。
func (s *Store) baselineUpdated(backend SyncBackend, content []byte, version string, remoteExisted, localExisted bool) syncBaselineV1 {
	return syncBaselineV1{
		Version: 1, Backend: backend.BackendID(), ContentSHA256: contentHash(content), RemoteVersion: version,
		RemoteNil: !remoteExisted, LocalNil: !localExisted, Content: content,
		Device: deviceName(), UpdatedAt: s.now().Format(time.RFC3339),
	}
}

// syncAdopt 把远端内容写入本地磁盘（校验通过才跟随，R8）：verifyDisk →
// 原子替换 → 更新磁盘指纹 → 基线 → 冻结未重载期间的保存并请求界面重载。
// merged 表示这是合并后恰好等于远端的结果（界面提示用）。
func (s *Store) syncAdopt(remote []byte, version string, remoteExisted bool, rev0 uint64, merged bool) error {
	if err := s.verifyDisk(); err != nil {
		// 磁盘与记录指纹不符（外部改动/移动/删除）：不安全跟随。
		return fmt.Errorf("远端内容未能安全落盘：%w", err)
	}
	// 判定与写入之间又出现新的本地快照：中止本轮（不丢输入），下一轮重判。
	if err := s.syncCheckNoNewSaves(rev0); err != nil {
		return err
	}
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
	now := s.now()
	s.syncState.LastRemoteApply = now
	if merged {
		s.syncState.LastMerge = now
	}
	s.mu.Unlock()
	var content []byte
	if remoteExisted {
		content = remote
	}
	return s.saveSyncBaseline(syncBaselineV1{
		Version: 1, Backend: s.backendIDForBase(), ContentSHA256: contentHash(content),
		RemoteVersion: version, RemoteNil: !remoteExisted, LocalNil: !remoteExisted, Content: content,
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

func (s *Store) backendIDForBase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncBackend == nil {
		return ""
	}
	return s.syncBackend.BackendID()
}

// decideSync 是判定表的纯函数（「已变」指与本地基线比较）。从未同步过的
// 设备：远端有文件就以云端为准（新设备上的欢迎文本不应覆盖云端笔记），
// 远端没有就上传本机。双方都改且内容不同则合并（ADR-0004）。
func decideSync(
	localExisted bool, localHash string,
	remoteExisted bool, remoteHash string,
	base syncBaselineV1,
) syncDecision {
	var localChanged, remoteChanged bool
	if base.neverSynced() {
		switch {
		case remoteExisted && localExisted && localHash == remoteHash:
			return syncUpdateBaseline
		case remoteExisted:
			return syncAdoptRemote
		case localExisted:
			return syncUploadLocal
		}
		return syncNone
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
		// 一边删了整个文件、另一边改了：保留有内容的一边，不静默丢内容。
		if !remoteExisted {
			return syncUploadLocal
		}
		if !localExisted {
			return syncAdoptRemote
		}
		return syncMerge
	}
	return syncNone
}

// loadSyncBaseline 读取基线状态文件；缺失或损坏按「从未同步」处理（以云端
// 为准），损坏时记录日志。
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
	s.syncTimer = time.AfterFunc(syncSaveDebounce, func() {
		// 触发后先清空字段再入轮：否则字段残留会永久阻止后续防抖装填
		// （探针实测发现的缺陷）。
		s.mu.Lock()
		s.syncTimer = nil
		s.mu.Unlock()
		slog.Info("保存后防抖同步轮触发")
		s.SyncNow()
	})
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

// contentHash 返回内容的 sha256 hex；nil 表示无文件，返回空串。
func contentHash(content []byte) string {
	if content == nil {
		return ""
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// contentHashPrefix 把指纹截短为日志友好的前缀；空串（无文件）原样返回。
func contentHashPrefix(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// deviceName 返回本机名（基线诊断与提交说明用）；失败回退 unknown。
func deviceName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "unknown"
	}
	return strings.TrimSpace(name)
}
