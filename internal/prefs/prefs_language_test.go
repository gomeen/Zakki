package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPreferencesLanguageValidation 界面语言的取值白名单：四种语言与空串
// （跟随系统）都合法，未知语言既不通过校验，也不会被静默重置成默认值。
func TestPreferencesLanguageValidation(t *testing.T) {
	for _, lang := range []string{"", "en", "ja", "zh-Hans", "zh-Hant"} {
		p := Default()
		p.Appearance.Language = lang
		if err := p.Validate(nil); err != nil {
			t.Fatalf("语言 %q 应通过校验: %v", lang, err)
		}
	}
	for _, lang := range []string{"fr", "ja-JP", "zh-Hant-TW", "zh-Hant "} {
		p := Default()
		p.Appearance.Language = lang
		if err := p.Validate(nil); err == nil {
			t.Fatalf("语言 %q 应被拒绝: %+v", lang, p.Appearance)
		}
	}
}

// TestPreferencesLanguageRoundTrip 合法语言经 Store 落盘后原样读回，不会被
// 迁移或校验重置成默认值。
func TestPreferencesLanguageRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	for _, lang := range []string{"ja", "zh-Hant"} {
		p := Default()
		p.Appearance.Language = lang
		if err := NewStore(path, 0).SaveNow(p); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(raw, nil)
		if err != nil {
			t.Fatalf("语言 %q 落盘后应能读回: %v", lang, err)
		}
		if got.Appearance.Language != lang {
			t.Fatalf("语言 %q 被重置为 %q", lang, got.Appearance.Language)
		}
	}
}

// TestPreferencesLanguageInvalidRejected 非法语言不是「重置为默认」而是报错：
// Decode 返回零值偏好与错误，调用方据此保留原文件并标记偏好损坏。
func TestPreferencesLanguageInvalidRejected(t *testing.T) {
	got, err := Decode([]byte(`{"version":15,"appearance":{"language":"fr"}}`), nil)
	if err == nil {
		t.Fatalf("非法语言应被拒绝: %+v", got.Appearance)
	}
	if got.Version != 0 || got.Appearance.Language != "" {
		t.Fatalf("解码失败应返回零值偏好: %+v", got)
	}
}
