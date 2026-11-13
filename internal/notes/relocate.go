package notes

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"zakki/internal/atomicfile"
)

// RelocateResult 描述迁址是否已经提交。Committed 为 true 后，权威路径和偏好均已切换，
// 后续错误只表示清理或持久性同步失败，调用方不得回退运行时偏好。
type RelocateResult struct {
	Committed bool
}

// RelocateCleanupError 表示迁址已经提交，但旧文件未能完成清理。
type RelocateCleanupError struct {
	Err error
}

func (e *RelocateCleanupError) Error() string { return e.Err.Error() }
func (e *RelocateCleanupError) Unwrap() error { return e.Err }

// Relocate 串行保存当前快照、校验新文件、持久化偏好，再切换权威路径。
// persist 只能操作调用方已拍下的 prefs.Store 快照，不得回调本 Store。
// 偏好成功后即使旧文件删除失败，结果也会报告已提交，且 DataPath 保持新路径。
func (s *Store) Relocate(target string, f File, persist func() error) (RelocateResult, error) {
	target, err := canonicalPath(target)
	if err != nil {
		s.ReportPersist("迁址失败", err)
		return RelocateResult{}, err
	}
	s.mu.Lock()
	if target == s.dataPath {
		s.mu.Unlock()
		return RelocateResult{}, nil
	}
	if s.stopped || s.closed {
		s.mu.Unlock()
		return RelocateResult{}, errors.New("存储已停止")
	}
	req := s.acceptLocked(cloneFile(f))
	s.pending = nil
	s.stopTimerLocked()
	s.mu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	old := s.DataPath()
	if target == old {
		return RelocateResult{}, nil
	}
	result, err := s.relocateLocked(old, target, req, persist)
	s.ReportPersist("迁址失败", err)
	return result, err
}

func (s *Store) relocateLocked(old, target string, req *pendingSave, persist func() error) (RelocateResult, error) {
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return RelocateResult{}, fmt.Errorf("目标文件已存在：%s", target)
		}
		return RelocateResult{}, err
	}
	if err := s.saveRequest(req); err != nil {
		return RelocateResult{}, err
	}
	s.mu.Lock()
	raw := bytes.Clone(s.savedContent)
	s.mu.Unlock()
	if err := writeExclusive(target, raw); err != nil {
		return RelocateResult{}, err
	}
	cleanup := func(cause error) error {
		if err := os.Remove(target); err != nil {
			return errors.Join(cause, fmt.Errorf("清理未采用目标失败：%w", err))
		}
		return cause
	}
	check := func() error {
		resolved, err := canonicalPath(target)
		if err != nil {
			return err
		}
		if resolved != target {
			return ErrExternalChange
		}
		got, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, raw) {
			return ErrExternalChange
		}
		return s.verifyDisk()
	}
	if err := check(); err != nil {
		return RelocateResult{}, cleanup(err)
	}
	// 本地 Git 命令与切换互斥；网络推送不占此门闩，旧推送由取消上下文中止。
	s.gitGate.Lock()
	defer s.gitGate.Unlock()
	if err := check(); err != nil {
		return RelocateResult{}, cleanup(err)
	}
	var persistErr error
	if persist != nil {
		persistErr = persist()
		if persistErr != nil && !atomicfile.IsCommitted(persistErr) {
			return RelocateResult{}, cleanup(persistErr)
		}
	}
	s.mu.Lock()
	s.dataPath = target
	s.diskKnown, s.diskExists = true, true
	s.diskHash = sha256.Sum256(raw)
	s.syncHold = false // 迁址整体替换了磁盘状态，解除同步冻结
	s.invalidateGitLocked(true)
	s.armGitLocked()
	s.mu.Unlock()
	result := RelocateResult{Committed: true}
	if persistErr != nil {
		return result, fmt.Errorf("已切换，偏好目录同步失败，旧文件已保留：%w", persistErr)
	}
	// 偏好已提交，不再回退路径；外部修改过的旧文件不得删除。
	oldRaw, err := os.ReadFile(old)
	if err != nil {
		return result, &RelocateCleanupError{Err: fmt.Errorf("已切换，读取旧文件失败：%w", err)}
	}
	if !bytes.Equal(oldRaw, raw) {
		return result, &RelocateCleanupError{Err: fmt.Errorf("已切换，旧文件发生外部修改：%w", ErrExternalChange)}
	}
	if err := s.stateRemove(old); err != nil {
		return result, &RelocateCleanupError{Err: fmt.Errorf("已切换，删除旧文件失败：%w", err)}
	}
	return result, nil
}

// writeExclusive 通过同目录临时文件和排他发布拒绝覆盖竞态中新建的目标。
func writeExclusive(path string, raw []byte) error {
	return writeExclusiveWithLink(path, raw, os.Link)
}

func writeExclusiveWithLink(path string, raw []byte, link func(string, string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 前缀与 internal/app 启动清扫（relocateTempPrefix）一致，改名须同步。
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zakki-relocate-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return err
		}
		if err = writeExclusiveFallback(path, raw); err != nil {
			return err
		}
	}
	// 候选文件的目录项必须先落盘，随后才允许偏好指向它并清理旧文件。
	if err = atomicfile.SyncDir(filepath.Dir(path)); err != nil {
		if cleanupErr := os.Remove(path); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("移除未提交候选文件失败: %w", cleanupErr))
		}
		return err
	}
	return nil
}

// writeExclusiveFallback 在不支持硬链接的文件系统上排他创建并回读验证。
func writeExclusiveFallback(path string, raw []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if cleanupErr := os.Remove(path); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("移除未提交候选文件失败: %w", cleanupErr))
			}
		}
	}()
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, raw) {
		return ErrExternalChange
	}
	return nil
}
