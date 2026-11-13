package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"zakki/internal/atomicfile"
)

// 启动清扫只删足够旧的已知临时文件，正式文件与正在写的新临时文件不动。
func TestSweepStaleTempsRemovesCrashLeftovers(t *testing.T) {
	a := newTestApp(t)
	if err := a.saveNow(); err != nil {
		t.Fatal(err)
	}
	if err := a.savePreferencesNow(); err != nil {
		t.Fatal(err)
	}
	dir := a.dataDir()
	now := a.now()
	old, fresh := now.Add(-time.Hour), now.Add(-time.Minute)
	age := func(path string, mod time.Time) {
		t.Helper()
		persistWrite(t, path, "残留")
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	stale := []string{
		filepath.Join(dir, atomicfile.TempPrefix(a.notes.DataPath())+"1"),
		filepath.Join(dir, atomicfile.TempPrefix(a.preferencesPath())+"1"),
		filepath.Join(dir, atomicfile.TempPrefix(a.syncStatePath())+"1"),
		filepath.Join(dir, relocateTempPrefix+"1"),
		filepath.Join(dir, writeProbeTempPrefix+"1"),
	}
	for _, p := range stale {
		age(p, old)
	}
	inFlight := filepath.Join(dir, atomicfile.TempPrefix(a.notes.DataPath())+"2")
	age(inFlight, fresh)

	a.sweepStaleTemps()

	for _, p := range stale {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("崩溃残留未清理: %s (%v)", filepath.Base(p), err)
		}
	}
	for _, p := range []string{inFlight, a.notes.DataPath(), a.preferencesPath()} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("不应删除 %s: %v", filepath.Base(p), err)
		}
	}
}
