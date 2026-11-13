package atomicfile

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func writeAged(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// 只删前缀匹配且足够旧的普通文件；新临时文件、正式文件、目录一律不动。
func TestRemoveStaleTempsOnlyOldMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	old, fresh := now.Add(-time.Hour), now.Add(-time.Second)
	prefix := TempPrefix(filepath.Join(dir, "zakki.json"))

	writeAged(t, filepath.Join(dir, prefix+"old"), old)
	writeAged(t, filepath.Join(dir, ".zakki-relocate-1"), old)
	writeAged(t, filepath.Join(dir, prefix+"fresh"), fresh)
	writeAged(t, filepath.Join(dir, "zakki.json"), old)
	writeAged(t, filepath.Join(dir, ".other.tmp-1"), old)
	if err := os.Mkdir(filepath.Join(dir, prefix+"dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveStaleTemps(dir, []string{prefix, ".zakki-relocate-"}, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(removed)
	if want := []string{prefix + "old", ".zakki-relocate-1"}; !reflect.DeepEqual(removed, sortedCopy(want)) {
		t.Fatalf("删除列表 = %v, want %v", removed, sortedCopy(want))
	}
	want := sortedCopy([]string{prefix + "fresh", prefix + "dir", "zakki.json", ".other.tmp-1"})
	if got := dirNames(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("剩余 = %v, want %v", got, want)
	}
}

func TestRemoveStaleTempsMissingDir(t *testing.T) {
	removed, err := RemoveStaleTemps(filepath.Join(t.TempDir(), "none"), []string{".x"}, time.Now())
	if err != nil || removed != nil {
		t.Fatalf("目录不存在应无操作: %v %v", removed, err)
	}
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
