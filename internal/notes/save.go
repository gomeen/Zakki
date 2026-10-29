package notes

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"time"

	"zakki/core/model"
	"zakki/internal/atomicfile"
)

type pendingSave struct {
	revision uint64
	file     File
	interval time.Duration
	after    func()
	ready    bool
}

func cloneFile(f File) File {
	f.Tabs = append([]Tab(nil), f.Tabs...)
	f.Dots = append([]string(nil), f.Dots...)
	return f
}

func (s *Store) acceptLocked(f File) *pendingSave {
	s.saveRevision++
	return &pendingSave{revision: s.saveRevision, file: f}
}

// ErrStopped 表示最终保存已开始，新的同步保存未被接受。
var ErrStopped = errors.New("笔记存储已停止，请在恢复保存后重试")

// SaveNow 同步保存；更新请求仍可在慢写期间排队。
func (s *Store) SaveNow(f File) error {
	f = cloneFile(f)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	req := s.acceptLocked(f)
	s.pending = nil
	s.stopTimerLocked()
	s.mu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return ErrStopped
	}
	return s.saveRequest(req)
}

// ScheduleSave 从首笔改动开始计时，后续输入只替换快照，不延长期限。
// interval 非正数表示立即保存；应用通常传入两秒。
func (s *Store) ScheduleSave(f File, interval time.Duration, after func()) {
	f = cloneFile(f)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.closed {
		return
	}
	ready := s.pending != nil && s.pending.ready
	req := s.acceptLocked(f)
	req.interval, req.after, req.ready = interval, after, ready
	s.pending = req
	if s.saveTimer == nil && !ready {
		s.armSaveLocked(interval)
	}
}

func (s *Store) armSaveLocked(delay time.Duration) {
	s.timerGen++
	gen := s.timerGen
	s.saveTimer = time.AfterFunc(delay, func() { s.flushPending(gen) })
}

func (s *Store) flushPending(gen uint64) {
	s.mu.Lock()
	if gen != s.timerGen || s.stopped || s.closed || s.pending == nil {
		s.mu.Unlock()
		return
	}
	s.saveTimer = nil
	s.pending.ready = true
	if s.saveRunning {
		s.mu.Unlock()
		return
	}
	s.saveRunning = true
	s.mu.Unlock()
	for {
		s.writeMu.Lock()
		s.mu.Lock()
		req := s.pending
		if s.stopped || s.closed || req == nil || !req.ready {
			s.saveRunning = false
			s.mu.Unlock()
			s.writeMu.Unlock()
			return
		}
		s.pending = nil
		s.mu.Unlock()
		err := s.saveRequest(req)
		s.mu.Lock()
		if err != nil && s.pending == nil && !s.stopped && !s.closed {
			req.ready = false
			s.pending = req
			delay := req.interval
			if delay <= 0 {
				delay = 2 * time.Second
			}
			s.armSaveLocked(delay)
		}
		s.mu.Unlock()
		s.writeMu.Unlock()
		if req.after != nil {
			req.after()
		}
	}
}

func (s *Store) saveRequest(req *pendingSave) error {
	s.mu.Lock()
	if req.revision < s.saveCompleted || req.revision == s.saveCompleted && s.saveResult == nil {
		err := s.saveResult
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	err := s.writeFile(req.file)
	s.completeSave(req.revision, err)
	s.ReportPersist("保存失败", err)
	return err
}

// completeSave 在写锁内发布，失败也淘汰旧请求，防止旧成功掩盖新失败。
func (s *Store) completeSave(revision uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCompleted, s.saveResult = revision, err
	if err == nil {
		s.saveSaved = revision
		s.armGitLocked()
	}
}

// encodeDisk 把文件与载入时捕获的三层未知字段快照组装后交给共享编码器：
// 快照只在锁内读取，Encode 不改 map；TabUnknown 只输出 f.Tabs 里仍存在
// 的 id，删除的标签连同其未知字段不再落盘，新建标签天然没有未知字段。
func (s *Store) encodeDisk(f File) ([]byte, error) {
	s.mu.Lock()
	doc := model.Document{
		File:            f,
		Unknown:         s.topUnknown,
		SettingsUnknown: s.settingsUnknown,
		TabUnknown:      s.tabUnknown,
	}
	s.mu.Unlock()
	return model.Encode(doc)
}

func (s *Store) writeFile(f File) error {
	raw, err := s.encodeDisk(f)
	if err != nil {
		return err
	}
	if err = s.verifyDisk(); err != nil {
		return err
	}
	s.mu.Lock()
	// 已发布但未同步目录的内容仍须重写，不能因字节相同提前报成功。
	same := bytes.Equal(raw, s.savedContent) && s.diskExists && !s.syncPending
	path := s.dataPath
	s.mu.Unlock()
	if same {
		return nil
	}
	if err = s.stateWrite(path, raw); err != nil && !atomicfile.IsCommitted(err) {
		return err
	}
	s.mu.Lock()
	s.diskKnown, s.diskExists = true, true
	s.diskHash, s.savedContent = sha256.Sum256(raw), raw
	s.syncPending = err != nil
	s.lastSaveTime = s.now()
	s.mu.Unlock()
	return err
}

// resetLoadedSaveLocked 使载入前已接受但尚未写入的请求全部过期。
func (s *Store) resetLoadedSaveLocked() {
	s.pending = nil
	s.stopTimerLocked()
	s.saveRevision++
	s.saveCompleted, s.saveSaved, s.saveResult = s.saveRevision, s.saveRevision, nil
}

func (s *Store) stopTimerLocked() {
	s.timerGen++
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
}

// StopSaveTimer 取消尚未开始的定时保存，不丢弃未保存状态。
func (s *Store) StopSaveTimer() { s.mu.Lock(); s.stopTimerLocked(); s.mu.Unlock() }

// Shutdown 等待在途写入并保存最终快照；失败后可重试，不提前报告退出成功。
func (s *Store) Shutdown(f File) error {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()
	f = cloneFile(f)
	s.mu.Lock()
	if s.stopped && s.saveResult == nil && s.saveSaved == s.saveRevision {
		s.mu.Unlock()
		return nil
	}
	s.stopTimerLocked()
	s.stopped = true
	s.pending = nil
	req := s.acceptLocked(f)
	s.mu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.saveRequest(req)
}
