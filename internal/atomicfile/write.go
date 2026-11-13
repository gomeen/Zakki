package atomicfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// CommitError 表示目标文件已经完成原子替换，但后续持久化同步失败。
type CommitError struct {
	Err error
}

func (e *CommitError) Error() string { return e.Err.Error() }
func (e *CommitError) Unwrap() error { return e.Err }

// IsCommitted 报告错误发生前目标文件是否已经完成原子替换。
func IsCommitted(err error) bool {
	var committed *CommitError
	return errors.As(err, &committed)
}

// WriteJSON 以同目录临时文件原子替换 JSON 文件，并在返回前完成落盘同步。
func WriteJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteBytes(path, append(raw, '\n'))
}

// WriteBytes 以同目录随机临时文件原子替换目标，并在返回前完成落盘同步。
func WriteBytes(path string, data []byte) error {
	return writeBytes(path, data, SyncDir)
}

func writeBytes(path string, data []byte, syncDir func(string) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, TempPrefix(path)+"*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
		}
		if !published {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("设置临时文件权限: %w", err)
	}
	if n, err := tmp.Write(data); err != nil {
		return fmt.Errorf("写入临时文件: %w", err)
	} else if n != len(data) {
		return fmt.Errorf("写入临时文件: %w", io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("同步临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	tmp = nil
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("发布目标文件: %w", err)
	}
	published = true
	if err := syncDir(dir); err != nil {
		return &CommitError{Err: fmt.Errorf("同步目标目录: %w", err)}
	}
	return nil
}

// SyncDir 同步目录项变更。Windows 标准库不提供目录句柄同步；
// 文件内容由写入方在发布前同步。
func SyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := parent.Sync(); err != nil {
		_ = parent.Close()
		return err
	}
	return parent.Close()
}
