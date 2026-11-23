package app

// 「AI 助理」设置页：页面渲染、偏好校验联动、密钥状态刷新与错误文案映射。

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"zakki/internal/ai"
	"zakki/internal/notes"
	"zakki/locales"
)

// fakeAIKeyring 注入内存安全存储。
type fakeAIKeyring struct{ value []byte }

func (f *fakeAIKeyring) Load() ([]byte, error) {
	if f.value == nil {
		return nil, notes.ErrKeyringEmpty
	}
	return append([]byte(nil), f.value...), nil
}
func (f *fakeAIKeyring) Store(v []byte) error { f.value = append([]byte(nil), v...); return nil }
func (f *fakeAIKeyring) Delete() error        { f.value = nil; return nil }

func withAIKeyring(t *testing.T, k notes.Keyring) {
	t.Helper()
	old := systemAIKeyringFactory
	systemAIKeyringFactory = func(string) (notes.Keyring, error) { return k, nil }
	t.Cleanup(func() { systemAIKeyringFactory = old })
}

func TestAIPageRenders(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.settingsPage = pageAI
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	for _, want := range []string{
		"AI 助理", "启用 AI 助理", "提供方", "接口地址", "模型",
		"API 密钥", "测试连接", "钥匙串",
	} {
		if !tt.HasText(want) {
			t.Fatalf("AI 页缺少 %q: %v", want, tt.Texts())
		}
	}
}

func TestAIPageDraftAppliesWithValidation(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	// 开关先行：未配置就开启也应成功（非空字段才校验格式），
	// aiConfigured 仍为 false，发送时才拦截并引导去设置。
	if err := a.updatePreferences(func(p *preferences) { p.AI.Enabled = true }); err != nil {
		t.Fatalf("无端点无模型的开启应成功（开关先行）: %v", err)
	}
	if err := a.updatePreferences(func(p *preferences) { p.AI.Enabled = true; p.AI.Provider = "grok" }); err == nil {
		t.Fatal("未知提供方仍应被拒绝（非空字段校验格式）")
	}
	if a.aiConfigured() {
		t.Fatal("未配置时 aiConfigured 应为 false")
	}
	if err := a.updatePreferences(func(p *preferences) {
		p.AI.Enabled = true
		p.AI.Provider = "openai"
		p.AI.BaseURL = "https://api.openai.com/v1"
		p.AI.Model = "gpt-4o-mini"
	}); err != nil {
		t.Fatalf("完整配置应通过校验: %v", err)
	}
	if !a.aiConfigured() {
		t.Fatal("配置完成后 aiConfigured 应为 true")
	}
	if a.prefs.AI.Provider != "openai" || a.prefs.AI.Model != "gpt-4o-mini" {
		t.Fatalf("配置应落盘: %+v", a.prefs.AI)
	}
}

func TestAIKeyStateRefresh(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	k := &fakeAIKeyring{}
	withAIKeyring(t, k)
	a.refreshAIKeyState()
	if a.ai.keyConfigured {
		t.Fatal("空存储不应报已配置")
	}
	if err := notes.SaveAIKey(k, "sk-1"); err != nil {
		t.Fatal(err)
	}
	a.refreshAIKeyState()
	if !a.ai.keyConfigured {
		t.Fatal("保存后应报已配置")
	}
	// aiClient 的密钥供给闭包读同一存储。
	if _, err := notes.LoadAIKey(k); err != nil {
		t.Fatal(err)
	}
}

func TestAIErrTextMapping(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	cases := []struct {
		err  error
		want string
	}{
		{ai.ErrNotConfigured, locales.AiErrNotConfigured()},
		{ai.ErrAuth, locales.AiErrAuth()},
		{ai.ErrCancelled, locales.AiErrCancelled()},
		{ai.ErrParse, locales.AiErrParse()},
		{ai.ErrNoProposal, locales.AiErrNoProposal()},
		{ai.ErrAnchorNotFound, locales.AiErrAnchor()},
		{&ai.RateLimitError{}, locales.AiErrRateLimit()},
	}
	for _, tc := range cases {
		if got := aiErrText(tc.err); got != tc.want {
			t.Errorf("aiErrText(%v) got %q want %q", tc.err, got, tc.want)
		}
	}
	if got := aiErrText(errors.New("别的错误")); got != "别的错误" {
		t.Errorf("未分类错误应带原文: %q", got)
	}
}
