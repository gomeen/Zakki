package app

// AI 助理桥接：客户端装配、密钥（系统安全存储，service "Zakki AI"）与
// 连接测试。密钥绝不进偏好或笔记文件（R7 语义）；后台 goroutine 只拿
// 主线程拍下的不可变输入（配置、密钥实例），完成经 runOnMain 回主线程。

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"zakki/internal/ai"
	"zakki/internal/notes"
	"zakki/locales"
)

// systemAIKeyringFactory 供测试注入内存实现；生产经 notes.SystemAIKeyring。
var systemAIKeyringFactory = notes.SystemAIKeyring

// aiAssistState AI 助理运行态（主线程）：安全存储、连接测试与助理条会话
// （ai_assistant.go / view_ai_bar.go）。
type aiAssistState struct {
	keyring       notes.Keyring // 懒创建的安全存储；nil = 不可用
	keyConfigured bool          // 密钥是否已在安全存储（打开 AI 页时刷新）
	keyBusy       bool          // 密钥写入/清除进行中
	testing       bool          // 连接测试进行中
	testResult    string        // 行内结果；空 = 无结果
	testErr       bool          // 结果是否为失败
	listing       bool
	models        []string
	modelsErr     string
	modelsSource  aiSettingsSource
	manualModel   bool
	modelRequest  uint64
	modelCancel   context.CancelFunc
	testRequest   uint64
	testSource    aiSettingsSource
	testModel     string
	bar           aiBarState // 助理条会话
	asker         aiAsker    // 非空时替代 aiClient（测试注入）
	enabledPrev   bool       // 上一次偏好应用时的开关：从关到开时默认显示助理条
}

// aiKeyring 懒创建 AI 密钥的安全存储（失败只记日志，调用处如实展示）。
func (a *app) aiKeyring() notes.Keyring {
	if a.ai.keyring != nil {
		return a.ai.keyring
	}
	k, err := systemAIKeyringFactory(a.dataDir())
	if err != nil {
		slog.Warn("AI 安全存储不可用", "error", err)
		return nil
	}
	a.ai.keyring = k
	return k
}

// aiClient 按当前偏好装配客户端。密钥供给闭包捕获此刻的 keyring 实例，
// 不在后台触碰 app 状态；无安全存储时不带鉴权（无法用需要密钥的服务）。
func (a *app) aiClient() *ai.Client {
	return a.aiClientFor(strings.TrimSpace(a.prefs.AI.BaseURL), strings.TrimSpace(a.prefs.AI.Model))
}

func (a *app) aiClientFor(baseURL, model string) *ai.Client {
	cfg := ai.Config{BaseURL: baseURL, Model: model}
	if k := a.aiKeyring(); k != nil {
		cfg.APIKey = func(context.Context) (string, error) {
			key, err := notes.LoadAIKey(k)
			if errors.Is(err, notes.ErrKeyringEmpty) {
				return "", nil
			}
			return key, err
		}
	}
	return ai.New(cfg)
}

// aiConfigured 报告 AI 助理是否已配置可用（开关 + 端点 + 模型）。
func (a *app) aiConfigured() bool {
	p := a.prefs.AI
	return p.Enabled && strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.Model) != ""
}

// refreshAIKeyState 主线程读一次安全存储，更新「密钥已配置」缓存。
func (a *app) refreshAIKeyState() {
	a.ai.keyConfigured = false
	if k := a.aiKeyring(); k != nil {
		if _, err := notes.LoadAIKey(k); err == nil {
			a.ai.keyConfigured = true
		}
	}
}

// aiSettingsDispatch 捕获设置窗投递入口；设置窗可以独立于主窗口运行。
func (a *app) aiSettingsDispatch() func(func()) {
	if a.settingsWin != nil {
		return a.settingsWin.Update
	}
	return a.runOnMain
}

// saveAIKey 后台把输入框里的密钥写入安全存储；成功后清空输入框。
// 密钥不落盘、不进日志，只经系统安全存储。
func (a *app) saveAIKey() {
	if a.ai.keyBusy {
		return
	}
	key := strings.TrimSpace(a.settingsUI.aiKeyText)
	if key == "" {
		return
	}
	k := a.aiKeyring()
	if k == nil {
		a.settingsUI.aiKeyErr = locales.AiKeyringUnavailable()
		a.refreshSettingsWindow()
		return
	}
	a.ai.keyBusy = true
	a.settingsUI.aiKeyErr = ""
	source := a.aiSettingsSource()
	dispatch := a.aiSettingsDispatch()
	go func() {
		err := notes.SaveAIKey(k, key)
		dispatch(func() {
			a.ai.keyBusy = false
			if err == nil {
				a.ai.invalidateConnection()
				a.settingsUI.aiKeyText = ""
				a.ai.keyConfigured = true
				a.showToast(locales.AiKeySaved())
				if source == a.aiSettingsSource() {
					a.loadSavedAIModels()
				}
			} else {
				a.settingsUI.aiKeyErr = err.Error()
			}
			a.refreshSettingsWindow()
		})
	}()
}

// clearAIKey 后台删除安全存储里的密钥（不影响偏好里的非凭据配置）。
func (a *app) clearAIKey() {
	if a.ai.keyBusy || !a.ai.keyConfigured {
		return
	}
	k := a.aiKeyring()
	if k == nil {
		return
	}
	a.ai.keyBusy = true
	a.settingsUI.aiKeyErr = ""
	dispatch := a.aiSettingsDispatch()
	go func() {
		err := notes.ClearAIKey(k)
		dispatch(func() {
			a.ai.keyBusy = false
			if err == nil {
				a.ai.invalidateConnection()
				a.ai.keyConfigured = false
				a.showToast(locales.AiKeyCleared())
				a.loadSavedAIModels()
			} else {
				a.settingsUI.aiKeyErr = err.Error()
			}
			a.refreshSettingsWindow()
		})
	}()
}

type aiSettingsSource struct {
	provider string
	baseURL  string
}

func (a *app) aiSettingsSource() aiSettingsSource {
	return aiSettingsSource{a.settingsUI.draft.AI.Provider, strings.TrimSpace(a.settingsUI.aiBaseURLText)}
}

func (a *app) currentAIModels() []string {
	if a.ai.modelsSource != a.aiSettingsSource() {
		return nil
	}
	return a.ai.models
}

func (s *aiAssistState) invalidateConnection() {
	s.modelRequest++
	if s.modelCancel != nil {
		s.modelCancel()
		s.modelCancel = nil
	}
	s.listing, s.manualModel = false, false
	s.models, s.modelsErr = nil, ""
	s.invalidateTest()
}

func (s *aiAssistState) invalidateTest() {
	s.testRequest++
	s.testing, s.testResult = false, ""
}

// validateAISettings 让草稿请求与偏好写入遵守同一地址校验，再读取密钥和联网。
func (a *app) validateAISettings(source aiSettingsSource, model string) error {
	p := a.prefs
	p.AI.Provider, p.AI.BaseURL, p.AI.Model = source.provider, source.baseURL, model
	return validatePrefs(p)
}

// testAIConnection 测试当前表单，未通过测试也不阻止启用。
func (a *app) testAIConnection() {
	if a.ai.testing || a.ai.keyBusy {
		return
	}
	source := a.aiSettingsSource()
	model := strings.TrimSpace(a.settingsUI.aiModelText)
	a.ai.testSource, a.ai.testModel = source, model
	if source.baseURL == "" || model == "" {
		a.ai.testResult, a.ai.testErr = locales.AiTestIncomplete(), true
		a.refreshSettingsWindow()
		return
	}
	if err := a.validateAISettings(source, model); err != nil {
		a.ai.testResult, a.ai.testErr = err.Error(), true
		a.refreshSettingsWindow()
		return
	}
	client := a.aiClientFor(source.baseURL, model)
	a.ai.testRequest++
	request := a.ai.testRequest
	a.ai.testing = true
	a.ai.testResult = ""
	a.refreshSettingsWindow()
	dispatch := a.aiSettingsDispatch()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := client.TestConnection(ctx)
		dispatch(func() {
			if request != a.ai.testRequest {
				return
			}
			a.ai.testing = false
			if source == a.aiSettingsSource() && model == strings.TrimSpace(a.settingsUI.aiModelText) {
				if err == nil {
					a.ai.testResult, a.ai.testErr = locales.AiTestOK(), false
				} else {
					a.ai.testResult, a.ai.testErr = locales.AiTestFailed(aiErrText(err)), true
				}
			}
			a.refreshSettingsWindow()
		})
	}()
}

// loadSavedAIModels 自动读取只使用已保存的连接配置，不向正在编辑的地址发送密钥。
func (a *app) loadSavedAIModels() {
	source := a.aiSettingsSource()
	if a.settingsPage == pageAI && source.baseURL != "" &&
		source == (aiSettingsSource{a.prefs.AI.Provider, strings.TrimSpace(a.prefs.AI.BaseURL)}) {
		a.fetchAIModels()
	}
}

func (a *app) fetchAIModels() {
	if a.ai.listing || a.ai.keyBusy {
		return
	}
	source := a.aiSettingsSource()
	if a.ai.modelsSource != source {
		a.ai.models = nil
	}
	a.ai.modelsSource = source
	if source.baseURL == "" {
		a.ai.modelsErr = locales.AiModelsNeedEndpoint()
		a.refreshSettingsWindow()
		return
	}
	if err := a.validateAISettings(source, ""); err != nil {
		a.ai.modelsErr = err.Error()
		a.refreshSettingsWindow()
		return
	}
	client := a.aiClientFor(source.baseURL, "")
	a.ai.modelRequest++
	request := a.ai.modelRequest
	a.ai.listing = true
	a.ai.modelsErr = ""
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	a.ai.modelCancel = cancel
	a.refreshSettingsWindow()
	dispatch := a.aiSettingsDispatch()
	go func() {
		defer cancel()
		ids, err := client.ListModels(ctx)
		dispatch(func() {
			a.finishAIModels(request, source, ids, err)
		})
	}()
}

func (a *app) finishAIModels(request uint64, source aiSettingsSource, ids []string, err error) {
	if request != a.ai.modelRequest {
		return
	}
	a.ai.listing = false
	a.ai.modelCancel = nil
	if source == a.aiSettingsSource() {
		if err != nil {
			a.ai.modelsErr = aiErrText(err)
		} else {
			a.ai.models = ids
		}
	}
	a.refreshSettingsWindow()
}

// aiErrText 把 ai 包的错误分类映射为本地化文案；未分类错误带原文呈现。
func aiErrText(err error) string {
	var rl *ai.RateLimitError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ai.ErrNotConfigured):
		return locales.AiErrNotConfigured()
	case errors.Is(err, ai.ErrAuth):
		return locales.AiErrAuth()
	case errors.Is(err, ai.ErrCancelled):
		return locales.AiErrCancelled()
	case errors.Is(err, ai.ErrParse):
		return locales.AiErrParse()
	case errors.Is(err, ai.ErrNoProposal):
		return locales.AiErrNoProposal()
	case errors.Is(err, ai.ErrAnchorNotFound):
		return locales.AiErrAnchor()
	case errors.As(err, &rl):
		return locales.AiErrRateLimit()
	case errors.Is(err, ai.ErrNetwork):
		return locales.AiErrNetwork(err.Error())
	}
	return err.Error()
}
