package app

// 启动清扫：进程在写入瞬间被杀会留下隐藏临时文件（原子写、迁址、可写探针），
// 正常路径会自己删掉。启动时删除足够旧的残骸，数据目录只留正式文件。

import (
	"log/slog"
	"path/filepath"
	"time"

	"zakki/internal/atomicfile"
)

// staleTempAge 远大于任何一次写入的耗时：共享文件夹（另一台设备、开发版）里
// 别的进程正在写的临时文件不会被误删。
const staleTempAge = 10 * time.Minute

// 与 notes.writeExclusive 和 validateStorageDir 创建临时文件的前缀一致。
const (
	relocateTempPrefix   = ".zakki-relocate-"
	writeProbeTempPrefix = ".zakki-write-probe-"
)

// sweepStaleTemps 在数据目录与笔记文件目录里清理崩溃残留。
// 只删已知前缀的隐藏文件；失败只记日志，不影响启动（不涉及用户数据的持久化）。
func (a *app) sweepStaleTemps() {
	cutoff := a.now().Add(-staleTempAge)
	dataPath := a.notes.DataPath()
	dirs := map[string][]string{}
	add := func(dir string, prefixes ...string) { dirs[dir] = append(dirs[dir], prefixes...) }
	add(a.dataDir(), atomicfile.TempPrefix(a.preferencesPath()), atomicfile.TempPrefix(a.syncStatePath()))
	add(filepath.Dir(dataPath), atomicfile.TempPrefix(dataPath), relocateTempPrefix, writeProbeTempPrefix)
	for dir, prefixes := range dirs {
		removed, err := atomicfile.RemoveStaleTemps(dir, prefixes, cutoff)
		if len(removed) > 0 {
			slog.Info("已清理崩溃残留的临时文件", "dir", dir, "files", removed)
		}
		if err != nil {
			slog.Warn("清理临时文件失败", "dir", dir, "error", err)
		}
	}
}
