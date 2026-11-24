package app

// 「AI 助理」设置页：页面渲染、偏好校验联动、密钥状态刷新与错误文案映射。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	if tt.HasText(locales.AiKeyClear()) || tt.HasText(locales.AiModelsFetch()) || tt.HasText(locales.AiModelManual()) {
		t.Fatal("密钥和模型操作不应再占用独立按钮行")
	}
	model, foundModel := tt.Find(locales.AiModel())
	actions, foundActions := tt.Find(locales.AiModelActions())
	if !foundModel || !foundActions || actions.X <= model.X || actions.Y >= model.Y+model.H || model.Y >= actions.Y+actions.H {
		t.Fatalf("模型菜单应与模型控件同行: model=%+v actions=%+v", model, actions)
	}
	var lastY float32 = -1
	for _, label := range []string{
		locales.AiEnable(), locales.AiProvider(), locales.AiBaseURL(), locales.AiKeyTitle(),
		locales.AiModel(), locales.AiTestButton(), locales.AiContextCurrentOnly(),
	} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y <= lastY {
			t.Fatalf("%q 未按配置顺序显示: %+v", label, bounds)
		}
		lastY = bounds.Y
	}
}

func TestAIPageModelInputModes(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.settingsPage = pageAI
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	a.settingsUI.aiBaseURLText = "http://localhost:11434/v1"
	a.ai.modelsSource = a.aiSettingsSource()
	a.ai.models = []string{"demo-model"}
	tt := ui.NewTester(a.settingsView, 900, 900)
	tt.Frame()
	if err := tt.Click(locales.AiModelActions()); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.AiModelManual()); err != nil {
		t.Fatal(err)
	}
	if !a.ai.manualModel {
		t.Fatal("拉取成功后仍应能切换到手动输入")
	}
	a.finishAIModels(a.ai.modelRequest, a.aiSettingsSource(), []string{"refreshed-model"}, nil)
	if !a.ai.manualModel {
		t.Fatal("刷新结果不能打断手动输入")
	}
	tt.Frame()
	if err := tt.Click(locales.AiModelActions()); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.AiModelChoose()); err != nil {
		t.Fatal(err)
	}
	if a.ai.manualModel {
		t.Fatal("应切回模型下拉")
	}
	a.settingsUI.aiBaseURLText = "http://localhost:12345/v1"
	if len(a.currentAIModels()) != 0 {
		t.Fatal("修改端点后不能继续使用旧模型列表")
	}
}

func TestAISettingsAutoLoadModelsOnEntry(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	t.Cleanup(a.ai.invalidateConnection)
	keyring := &fakeAIKeyring{}
	if err := notes.SaveAIKey(keyring, "test-key"); err != nil {
		t.Fatal(err)
	}
	withAIKeyring(t, keyring)
	type request struct{ method, path, auth string }
	requests := make(chan request, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- request{r.Method, r.URL.Path, r.Header.Get("Authorization")}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-from-service"}]}`))
	}))
	t.Cleanup(srv.Close)

	a.openSettings(pageAI)
	if a.ai.listing || a.ai.modelsErr != "" {
		t.Fatal("未配置地址时不应自动拉取，也不应报错")
	}
	if err := a.updatePreferences(func(p *preferences) {
		p.AI.BaseURL = srv.URL + "/v1"
		p.AI.Model = "saved-model"
	}); err != nil {
		t.Fatal(err)
	}
	a.openSettings(pageGeneral)
	tt := ui.NewTester(a.settingsView, 760, 620)
	if err := tt.Click(locales.PageAI()); err != nil {
		t.Fatal(err)
	}
	if !a.ai.listing || !tt.HasText(locales.AiModelsFetching()) {
		t.Fatal("进入已配置的 AI 页应显示模型正在加载")
	}
	select {
	case got := <-requests:
		if want := (request{"GET", "/v1/models", "Bearer test-key"}); got != want {
			t.Fatalf("自动拉取请求 = %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("没有收到自动拉取请求")
	}

	id := a.ai.modelRequest
	a.finishAIModels(id, a.aiSettingsSource(), []string{"model-from-service"}, nil)
	tt.Frame()
	a.openSettings(pageAI)
	if a.ai.modelRequest != id || a.prefs.AI.Model != "saved-model" || a.settingsUI.aiModelText != "saved-model" {
		t.Fatal("重绘或聚焦当前页不能重复拉取，加载结果不能改动已选模型")
	}
	a.settingsUI.aiBaseURLText = srv.URL + "/other"
	a.loadSavedAIModels()
	if a.ai.modelRequest != id {
		t.Fatal("不得自动向未保存的草稿地址发送请求")
	}
}

// observedAIKeyring 记录后台存取，不需要启动原生窗口事件循环。
type observedAIKeyring struct {
	fakeAIKeyring
	stored  chan struct{}
	deleted chan struct{}
}

func (k *observedAIKeyring) Store(v []byte) error {
	err := k.fakeAIKeyring.Store(v)
	close(k.stored)
	return err
}

func (k *observedAIKeyring) Delete() error {
	err := k.fakeAIKeyring.Delete()
	close(k.deleted)
	return err
}

func TestAIKeyEnterSavesAndContextMenuClears(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	keyring := &observedAIKeyring{stored: make(chan struct{}), deleted: make(chan struct{})}
	withAIKeyring(t, keyring)
	a.openSettings(pageAI)
	tt := ui.NewTester(a.settingsView, 760, 620)
	if err := tt.Click(locales.AiKeyLabel()); err != nil {
		t.Fatal(err)
	}
	tt.Type("test-key")
	if a.ai.keyBusy {
		t.Fatal("键入过程中不能保存不完整的密钥")
	}
	tt.Key(0, ui.KeyEnter)
	select {
	case <-keyring.stored:
		if key, err := notes.LoadAIKey(keyring); err != nil || key != "test-key" {
			t.Fatal("回车应把输入的密钥写入安全存储")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("回车未触发保存")
	}
	// 无原生窗口时直接模拟安全存储写入后的状态。
	a.ai.keyBusy = false
	a.settingsUI.aiKeyText = ""
	a.refreshAIKeyState()
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if a.ai.keyBusy || !a.ai.keyConfigured {
		t.Fatal("空输入回车不能覆盖或清除已存密钥")
	}
	if err := tt.RightClick(locales.AiKeyLabel()); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.AiKeyClear()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-keyring.deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("输入框菜单未清除安全存储中的密钥")
	}
}

func TestAISettingsDiscardStaleModels(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.settingsUI.aiBaseURLText = "https://first.example/v1"
	source := a.aiSettingsSource()
	request := a.ai.modelRequest
	a.ai.invalidateConnection()
	a.settingsUI.aiBaseURLText = "https://second.example/v1"
	a.ai.listing = true
	a.finishAIModels(request, source, []string{"old-model"}, nil)
	if !a.ai.listing || len(a.ai.models) != 0 {
		t.Fatal("旧请求不能覆盖新请求的状态或模型列表")
	}
}

func TestAISettingsRejectInsecureDraft(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.settingsUI.aiBaseURLText = "http://remote.example/v1"
	a.settingsUI.aiModelText = "demo-model"
	a.fetchAIModels()
	a.testAIConnection()
	if a.ai.listing || a.ai.modelsErr == "" || a.ai.testing || !a.ai.testErr {
		t.Fatal("模型列表与连接测试都必须在发请求前拒绝非法地址")
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
