package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TempPrefix 是 WriteBytes 为 path 创建的临时文件名前缀（同目录、隐藏）。
func TempPrefix(path string) string {
	return "." + filepath.Base(path) + ".tmp-"
}

// RemoveStaleTemps 删除 dir 中名字以任一 prefix 开头、修改时间早于 cutoff 的
// 普通文件，返回已删除的文件名。只用于清理进程在写入瞬间被杀留下的临时文件：
// cutoff 必须远早于一次写入的耗时，避免误删另一进程（开发版、另一台设备经
// 共享文件夹）正在写的文件。目录不存在不算错误；单个文件失败不中断其余清理。
func RemoveStaleTemps(dir string, prefixes []string, cutoff time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	var errs []error
	for _, e := range entries {
		if !e.Type().IsRegular() || !hasAnyPrefix(e.Name(), prefixes) {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, e.Name())
	}
	return removed, errors.Join(errs...)
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
