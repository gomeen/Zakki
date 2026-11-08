package prefs

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPreferencesMigrateSyncSection 锁定 v13 → v14 的升级路径：旧文件没有
// sync 节，解码即零值（关闭）；写回即为当前版本并带出 sync 节。
func TestPreferencesMigrateSyncSection(t *testing.T) {
	v13 := `{"version":13,"appearance":{"bodySize":18},"git":{"autoCommit":true,"autoPush":true}}`
	got, err := Decode([]byte(v13), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion {
		t.Fatalf("版本应升到 %d，当前 %d", CurrentVersion, got.Version)
	}
	if got.Sync != (Sync{}) {
		t.Fatalf("旧文件缺 sync 键应为零值（关闭）: %+v", got.Sync)
	}
	if !got.Git.AutoCommit || !got.Git.AutoPush {
		t.Fatalf("迁移不得改动 git 原开关: %+v", got.Git)
	}
	// 写回即为当前版本，sync 节完整落盘（无 omitempty）。
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"sync":{"enabled":false,"backend":"","repo":""}`) {
		t.Fatalf("写回应带完整 sync 节: %s", raw)
	}
	// v13 文件即使带已开启的 git 自动化，升级后同步仍是关闭状态。
	if got.Git.AutoCommit && got.Sync.Enabled {
		t.Fatal("升级不得自动开启云同步")
	}
}

// TestPreferencesSyncValidation 校验 sync 节取值：开启时必须有后端与合法仓库。
func TestPreferencesSyncValidation(t *testing.T) {
	valid := func(mutate func(s *Sync)) func(*Preferences) {
		return func(p *Preferences) { mutate(&p.Sync) }
	}
	cases := []struct {
		name   string
		mutate func(*Sync)
		ok     bool
	}{
		{"关闭+零值", func(*Sync) {}, true},
		{"关闭+记忆仓库", func(s *Sync) { s.Repo = "gomeen/notes" }, true},
		{"开启+github+仓库", func(s *Sync) { s.Enabled = true; s.Backend = SyncBackendGitHub; s.Repo = "gomeen/notes" }, true},
		{"开启缺后端", func(s *Sync) { s.Enabled = true; s.Repo = "gomeen/notes" }, false},
		{"开启缺仓库", func(s *Sync) { s.Enabled = true; s.Backend = SyncBackendGitHub }, false},
		{"未知后端", func(s *Sync) { s.Backend = "webdav" }, false},
		{"仓库缺 owner", func(s *Sync) { s.Repo = "notes" }, false},
		{"仓库多余斜杠", func(s *Sync) { s.Repo = "a/b/c" }, false},
		{"仓库非法字符", func(s *Sync) { s.Repo = "a b/c" }, false},
		{"仓库点段", func(s *Sync) { s.Repo = "../notes" }, false},
		{"仓库空串且关闭", func(s *Sync) {}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Default()
			valid(tc.mutate)(&p)
			err := p.Validate(testAccel)
			if tc.ok && err != nil {
				t.Fatalf("应通过校验: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("非法取值必须被拒绝: %+v", p.Sync)
			}
		})
	}
	// 严格解码：sync 节内未知键拒绝，null 拒绝。
	for _, raw := range []string{
		`{"sync":{"unknown":1}}`,
		`{"sync":null}`,
		`{"sync":{"enabled":null}}`,
		`{"sync":{"backend":null}}`,
		`{"sync":{"repo":null}}`,
		`{"sync":{"enabled":"true"}}`,
	} {
		if _, err := Decode([]byte(raw), testAccel); err == nil {
			t.Fatalf("非法 sync 节被接受: %s", raw)
		}
	}
	// 合法带值文件原样读回。
	got, err := Decode([]byte(`{"version":14,"sync":{"enabled":true,"backend":"github","repo":"gomeen/notes"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Sync.Enabled || got.Sync.Backend != SyncBackendGitHub || got.Sync.Repo != "gomeen/notes" {
		t.Fatalf("带值应原样读回: %+v", got.Sync)
	}
}
