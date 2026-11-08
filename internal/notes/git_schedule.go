package notes

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// GitConfig 将本地提交与分支推送分开控制。
type GitConfig struct {
	AutoCommit, AutoPush         bool
	CommitInterval, PushInterval time.Duration
}

// GitState 是当前权威文件对应仓库的同步状态。
type GitState struct {
	RepoRoot             string
	Missing              bool
	CommitTime, PushTime time.Time
	Error                string
}

// ConfigureGit 相同配置仅更新回调，不重置定时周期。
func (s *Store) ConfigureGit(cfg GitConfig, after func()) {
	if cfg.CommitInterval <= 0 {
		cfg.CommitInterval = time.Minute
	}
	if cfg.PushInterval <= 0 {
		cfg.PushInterval = 10 * time.Minute
	}
	s.gitGate.Lock()
	defer s.gitGate.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gitAfter = after
	if cfg == s.gitConfig || s.closed {
		return
	}
	s.gitConfig = cfg
	s.invalidateGitLocked(false)
	s.gitCommitAttempt, s.gitPushAttempt = time.Time{}, time.Time{}
	s.armGitLocked()
}

// GitStatus 返回同步状态副本。
func (s *Store) GitStatus() GitState { s.mu.Lock(); defer s.mu.Unlock(); return s.gitStatus }

func (s *Store) invalidateGitLocked(reset bool) {
	s.gitGen++
	s.gitQueued = false
	if s.gitCancel != nil {
		s.gitCancel()
		s.gitCancel = nil
	}
	if s.gitTimer != nil {
		s.gitTimer.Stop()
		s.gitTimer = nil
	}
	if reset {
		s.gitStatus = GitState{}
		s.gitCommitAttempt, s.gitPushAttempt = time.Time{}, time.Time{}
	} else {
		s.gitStatus.Error = ""
	}
}

// 调度使用真实单调时间，注入时钟仅负责用户可见时间，避免固定测试时钟卡住周期。
func (s *Store) armGitLocked() {
	if s.closed || s.saveBlocked || s.invalid || !s.diskExists || s.gitRunning || s.gitTimer != nil {
		return
	}
	if s.syncActive {
		// 云同步开启：旧 Git 调度器暂停（§2.2）。用户原开关保留在偏好里，
		// 关闭同步后 ConfigureSync 会重新装填。
		return
	}
	cfg := s.gitConfig
	if !cfg.AutoCommit && !cfg.AutoPush {
		return
	}
	delay := time.Duration(1<<63 - 1)
	now := time.Now()
	if cfg.AutoCommit {
		if s.gitCommitAttempt.IsZero() {
			s.gitCommitAttempt = now
		}
		delay = cfg.CommitInterval - now.Sub(s.gitCommitAttempt)
	}
	if cfg.AutoPush {
		if s.gitPushAttempt.IsZero() {
			s.gitPushAttempt = now
		}
		push := cfg.PushInterval - now.Sub(s.gitPushAttempt)
		if push < delay {
			delay = push
		}
	}
	if delay < 0 {
		delay = 0
	}
	gen := s.gitGen
	s.gitTimer = time.AfterFunc(delay, func() { s.startGit(gen) })
}

func (s *Store) startGit(gen uint64) {
	s.mu.Lock()
	if gen != s.gitGen || s.closed {
		s.mu.Unlock()
		return
	}
	s.gitTimer = nil
	if s.saveBlocked || s.invalid || !s.diskExists {
		s.mu.Unlock()
		return
	}
	if s.gitRunning {
		s.gitQueued = true
		s.mu.Unlock()
		return
	}
	s.gitRunning = true
	s.gitDone = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	s.gitCancel = cancel
	path := s.dataPath
	now := time.Now()
	commit := s.gitConfig.AutoCommit && now.Sub(s.gitCommitAttempt) >= s.gitConfig.CommitInterval
	push := s.gitConfig.AutoPush && now.Sub(s.gitPushAttempt) >= s.gitConfig.PushInterval
	if commit {
		s.gitCommitAttempt = now
	}
	if push {
		s.gitPushAttempt = now
	}
	s.mu.Unlock()
	go func() {
		s.gitStep(ctx, gen, path, commit, push)
		cancel()
		s.mu.Lock()
		s.gitRunning = false
		close(s.gitDone)
		if gen == s.gitGen {
			s.gitCancel = nil
		}
		s.gitQueued = false
		s.armGitLocked()
		after := s.gitAfter
		s.mu.Unlock()
		if after != nil {
			after()
		}
	}()
}

var errGitObsolete = errors.New("Git 任务已过期")

// ErrPushRejected 表示远端拒绝了当前分支历史，不能靠自动重试解决。
var ErrPushRejected = errors.New("Git 推送被远端拒绝")

func pushError(out string, err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(out + "\n" + err.Error())
	if strings.Contains(message, "[rejected]") || strings.Contains(message, "non-fast-forward") || strings.Contains(message, "fetch first") {
		return fmt.Errorf("%w：远端有本地未包含的提交；请先手动检查远端变更并同步、解决冲突后再推送（不会自动拉取或强推）：%w", ErrPushRejected, err)
	}
	return err
}

func (s *Store) gitValidLocked(gen uint64) bool {
	return gen == s.gitGen && !s.closed && !s.saveBlocked && !s.invalid
}

// gitCommand 对每条命令而非仅最终结果检查代号。本地写命令与开关/迁址互斥；
// 推送不占写锁或门闩，系统 runner 的上下文在配置失效或 Close 时立即取消。
// 注入的无上下文 runner 必须自行结束已启动的命令，后续命令仍受代号保护。
func (s *Store) gitCommand(ctx context.Context, gen uint64, dir string, args ...string) (string, error) {
	localWrite := len(args) > 0 && (args[0] == "add" || args[0] == "commit")
	if localWrite {
		s.gitGate.Lock()
		defer s.gitGate.Unlock()
	}
	s.mu.Lock()
	valid := s.gitValidLocked(gen)
	runner := s.RunGit
	s.mu.Unlock()
	if !valid || ctx.Err() != nil {
		return "", errGitObsolete
	}
	if runner != nil {
		return runner(dir, args...)
	}
	return runGitContext(ctx, dir, args...)
}

func (s *Store) gitStep(ctx context.Context, gen uint64, path string, commit, push bool) {
	runner := func(dir string, args ...string) (string, error) { return s.gitCommand(ctx, gen, dir, args...) }
	root, err := runner(filepath.Dir(path), "rev-parse", "--show-toplevel")
	if err != nil {
		s.publishGit(gen, "", false, false, err)
		return
	}
	root = strings.TrimSpace(root)
	if root == "" {
		s.publishGit(gen, "", false, false, errors.New("Git 仓库根目录为空"))
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		s.publishGit(gen, root, false, false, errors.New("笔记文件不在 Git 工作区内"))
		return
	}
	// 核对保存基线后立即释放写锁，慢 Git 与网络均不阻塞新的笔记保存。
	s.writeMu.Lock()
	err = s.verifyDisk()
	s.writeMu.Unlock()
	if err != nil {
		s.publishGit(gen, root, false, false, err)
		return
	}
	committed := false
	if commit {
		committed, err = runGitJob(runner, root, rel)
	}
	pushed := false
	if err == nil && push {
		out, pushErr := runner(root, "push")
		err = pushError(out, pushErr)
		pushed = err == nil
	}
	s.publishGit(gen, root, committed, pushed, err)
}

func (s *Store) publishGit(gen uint64, root string, commit, push bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.gitValidLocked(gen) {
		return
	}
	s.gitStatus.RepoRoot = root
	s.gitStatus.Missing = errors.Is(err, ErrGitUnavailable)
	s.gitStatus.Error = ""
	if err != nil && !s.gitStatus.Missing {
		s.gitStatus.Error = err.Error()
	}
	if commit {
		s.gitStatus.CommitTime = s.now()
	}
	if push {
		s.gitStatus.PushTime = s.now()
	}
}

// WaitGitIdle 仅等待已启动的 worker，不等待将来的周期任务。
func (s *Store) WaitGitIdle() {
	for {
		s.mu.Lock()
		if !s.gitRunning {
			s.mu.Unlock()
			return
		}
		done := s.gitDone
		s.mu.Unlock()
		<-done
	}
}

// Close 停止计时器并取消后台 Git，不等待网络退出，不代表最终保存。
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopTimerLocked()
	s.stopSyncTimerLocked()
	s.invalidateGitLocked(false)
	// 云同步在途轮次作废：最终保存（Shutdown）先于 Close 完成，此处不再
	// 等待网络轮次（与 Git 同一约定），随后的 publish 会因代号失效被丢弃。
	s.syncGen++
	s.syncQueued = false
	s.syncState.Running = false
	if s.syncCancel != nil {
		s.syncCancel()
		s.syncCancel = nil
	}
}
