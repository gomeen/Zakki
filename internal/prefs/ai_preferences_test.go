package prefs

// AI 助理偏好：版本 16 新增 ai 节的解码语义、校验矩阵与「密钥绝不落盘」
// 的结构性保障（AI 结构体没有密钥字段，编码产物不含任何凭据键）。

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAIPreferencesMigrateV15(t *testing.T) {
	v15 := `{"version":15,"appearance":{"colorTheme":"paper"},"sync":{"enabled":false}}`
	got, err := Decode([]byte(v15), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || CurrentVersion != 16 {
		t.Fatalf("版本应升到 16，当前 %d", got.Version)
	}
	if got.AI != (AI{}) {
		t.Fatalf("旧文件缺 ai 键应解码为零值（关闭），got %+v", got.AI)
	}
	if err := got.Validate(testAccel); err != nil {
		t.Fatalf("零值 AI 必须合法: %v", err)
	}
}

func TestAIPreferencesValidate(t *testing.T) {
	valid := func(mutate func(*AI)) bool {
		p := Default()
		mutate(&p.AI)
		return p.Validate(testAccel) == nil
	}
	cases := []struct {
		name string
		fn   func(*AI)
		want bool
	}{
		{"未开启时全空合法", func(a *AI) {}, true},
		{"合法完整配置", func(a *AI) {
			a.Enabled, a.Provider, a.BaseURL, a.Model = true, "openai", "https://api.openai.com/v1", "gpt-4o-mini"
		}, true},
		{"本地 http 合法", func(a *AI) {
			a.Enabled, a.Provider, a.BaseURL, a.Model = true, "ollama", "http://localhost:11434/v1", "qwen3"
		}, true},
		{"回环 IP http 合法", func(a *AI) {
			a.BaseURL, a.Model = "http://127.0.0.1:8080/v1", "m"
		}, true},
		{"远程 http 拒绝", func(a *AI) { a.BaseURL = "http://api.example.com/v1" }, false},
		{"未知提供方拒绝", func(a *AI) { a.Provider = "grok" }, false},
		{"开启缺模型拒绝", func(a *AI) {
			a.Enabled, a.BaseURL = true, "https://api.openai.com/v1"
		}, false},
		{"开启缺地址拒绝", func(a *AI) { a.Enabled, a.Model = true, "m" }, false},
		{"地址带空白拒绝", func(a *AI) { a.BaseURL = " https://api.openai.com/v1" }, false},
		{"非 URL 拒绝", func(a *AI) { a.BaseURL = "api.openai.com" }, false},
		{"未开启时半填校验仍生效", func(a *AI) { a.BaseURL = "ftp://x/v1" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := valid(tc.fn); got != tc.want {
				t.Fatalf("Validate 通过=%v want %v", got, tc.want)
			}
		})
	}
}

// TestAIPreferencesNeverEncodeSecrets AI 节没有任何密钥字段：编码产物
// 不含 apiKey / token 之类键，密钥只可能存在于系统安全存储（R7 语义）。
func TestAIPreferencesNeverEncodeSecrets(t *testing.T) {
	p := Default()
	p.AI = AI{Enabled: true, Provider: "custom", BaseURL: "https://example.com/v1", Model: "m"}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	for _, banned := range []string{"apikey", "api_key", "secret", "token", "password"} {
		if strings.Contains(lower, banned) {
			t.Fatalf("偏好编码不得出现凭据键 %q：%s", banned, raw)
		}
	}
	if !strings.Contains(string(raw), `"ai":{"enabled":true,"provider":"custom","baseUrl":"https://example.com/v1","model":"m"}`) {
		t.Fatalf("ai 节应按契约落盘：%s", raw)
	}
}
