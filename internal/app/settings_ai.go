package app

// 设置「AI 助理」页：启用开关、提供方预设、端点、模型名、API 密钥（只进
// 系统安全存储）与连接测试。开关与端点写 preferences.json 的 ai 节；
// 密钥绝不进偏好文件。文案一律经 locales.*。

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"zakki/internal/ai"
	"zakki/locales"
)

// aiPage 设置页主体。
func (a *app) aiPage(c *ui.Context) {
	ui.Scroll(c.Key("ai-controls")).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(0, 22, 0, 0).Children(func() {
			settingsSectionTitle(c, locales.AiSection())
			settingsLead(c, locales.AiSectionHint())
			a.aiEnableRow(c)
			sectionDivider(c)
			a.aiContextRow(c)
			sectionDivider(c)
			a.aiProviderRow(c)
			a.aiEndpointRow(c)
			a.aiModelRow(c)
			sectionDivider(c)
			a.aiKeyRows(c)
			sectionDivider(c)
			a.aiTestRow(c)
			ui.Box(c).Grow(1)
			a.cardResetFooter(c, "ai", pageAI)
		})
	})
}

// aiEnableRow 启用开关；开启要求端点与模型已配置（偏好校验兜底，
// 失败时回滚开关并在页内提示）。
func (a *app) aiEnableRow(c *ui.Context) {
	settingsRow(c, locales.AiEnable(), locales.AiEnableHint(), func() {
		if ui.Switch(c.Key("ai-enabled"), &a.settingsUI.draft.AI.Enabled).Label(locales.AiEnable()).Changed() {
			on := a.settingsUI.draft.AI.Enabled
			// 开启时顺带提交输入框里尚未回车的端点/模型草稿：避免用户填完
			// 直接开开关被校验拒绝、助理条不弹出的困扰。
			if err := a.updatePreferences(func(p *preferences) {
				p.AI.Enabled = on
				if on {
					if v := strings.TrimSpace(a.settingsUI.aiBaseURLText); v != "" {
						p.AI.BaseURL, a.settingsUI.draft.AI.BaseURL = v, v
					}
					if v := strings.TrimSpace(a.settingsUI.aiModelText); v != "" {
						p.AI.Model, a.settingsUI.draft.AI.Model = v, v
					}
				}
			}); err != nil {
				a.settingsUI.draft.AI.Enabled = a.prefs.AI.Enabled
				a.settingsUI.keyErr = err.Error()
				a.refreshSettingsWindow()
				return
			}
			a.settingsUI.keyErr = ""
		}
	})
}

// aiContextRow 上下文范围：默认回答问题时参考所有标签；开启后仅发送当前标签。
func (a *app) aiContextRow(c *ui.Context) {
	settingsRow(c, locales.AiContextCurrentOnly(), locales.AiContextCurrentOnlyHint(), func() {
		if ui.Switch(c.Key("ai-context-scope"), &a.settingsUI.draft.AI.ContextCurrentOnly).Label(locales.AiContextCurrentOnly()).Changed() {
			v := a.settingsUI.draft.AI.ContextCurrentOnly
			if err := a.updatePreferences(func(p *preferences) { p.AI.ContextCurrentOnly = v }); err != nil {
				a.settingsUI.draft.AI.ContextCurrentOnly = a.prefs.AI.ContextCurrentOnly
				a.settingsUI.keyErr = err.Error()
				a.refreshSettingsWindow()
				return
			}
			a.settingsUI.keyErr = ""
		}
	})
}

// aiProviderRow 提供方预设：切换预填端点（custom 保留用户输入）。
func (a *app) aiProviderRow(c *ui.Context) {
	settingsRow(c, locales.AiProvider(), locales.AiProviderHint(), func() {
		if boundedSelect(c, &a.settingsUI.aiProviderSel, aiProviderOptions()).Width(200).Changed() {
			id := aiProviderIDOfDisplay(a.settingsUI.aiProviderSel)
			a.settingsUI.draft.AI.Provider = id
			mutate := func(p *preferences) { p.AI.Provider = id }
			if preset, ok := ai.ProviderByID(id); ok && id != "custom" {
				a.settingsUI.draft.AI.BaseURL = preset.BaseURL
				a.settingsUI.aiBaseURLText = preset.BaseURL
				mutate = func(p *preferences) {
					p.AI.Provider = id
					p.AI.BaseURL = preset.BaseURL
				}
			}
			a.pushDraft(mutate)
		}
	})
}

// aiEndpointRow 接口地址：提交时去空白写入偏好。
func (a *app) aiEndpointRow(c *ui.Context) {
	settingsRow(c, locales.AiBaseURL(), locales.AiBaseURLHint(), func() {
		if ui.TextInput(c.Key("ai-baseurl"), &a.settingsUI.aiBaseURLText).Width(240).
			Placeholder("https://api.openai.com/v1").Submitted() {
			v := strings.TrimSpace(a.settingsUI.aiBaseURLText)
			a.settingsUI.draft.AI.BaseURL = v
			a.pushDraft(func(p *preferences) { p.AI.BaseURL = v })
		}
	})
}

// aiModelRow 模型名：手填，或拉取端点模型列表后下拉选择（保留手填兼容
// 任意 OpenAI 兼容服务）。选择即提交，免回车。
func (a *app) aiModelRow(c *ui.Context) {
	settingsRow(c, locales.AiModel(), locales.AiModelHint(), func() {
		if len(a.ai.models) > 0 {
			sel := a.settingsUI.aiModelText
			if boundedSelect(c, &sel, a.ai.models).Width(240).Changed() {
				a.settingsUI.aiModelText = sel
				a.settingsUI.draft.AI.Model = sel
				a.pushDraft(func(p *preferences) { p.AI.Model = sel })
			}
			return
		}
		if ui.TextInput(c.Key("ai-model"), &a.settingsUI.aiModelText).Width(240).
			Placeholder("gpt-4o-mini").Submitted() {
			v := strings.TrimSpace(a.settingsUI.aiModelText)
			a.settingsUI.draft.AI.Model = v
			a.pushDraft(func(p *preferences) { p.AI.Model = v })
		}
	})
	// 获取列表：拉取成功变下拉；失败行内提示（可重试）。
	ui.Row(c).Gap(8).PaddingY(6).Children(func() {
		if ui.Button(c.Key("ai-models-fetch"), locales.AiModelsFetch()).Disabled(a.ai.listing).Clicked() {
			a.fetchAIModels()
		}
		if a.ai.listing {
			ui.Text(c, locales.AiModelsFetching()).FontSize(12).TextColor(tMuted(c))
		} else if a.ai.modelsErr != "" {
			ui.Text(c, a.ai.modelsErr).FontSize(12).TextColor(c.Theme().Danger)
		} else if len(a.ai.models) > 0 {
			ui.Text(c, locales.AiModelsLoaded(strconv.Itoa(len(a.ai.models)))).FontSize(12).TextColor(tMuted(c))
		}
	})
}

// aiKeyRows API 密钥：密码式输入 + 保存到钥匙串 / 清除 + 状态行。
// 密钥只进系统安全存储；输入框不回显已存值。
func (a *app) aiKeyRows(c *ui.Context) {
	settingsSectionTitle(c, locales.AiKeyTitle())
	settingsLead(c, locales.AiKeyHint())
	settingsRow(c, locales.AiKeyLabel(), "", func() {
		ui.TextInput(c.Key("ai-key"), &a.settingsUI.aiKeyText).Width(240).
			Placeholder("sk-…")
	})
	ui.Row(c).Gap(8).PaddingY(6).Children(func() {
		if ui.Button(c.Key("ai-key-save"), locales.AiKeySave()).Disabled(a.ai.keyBusy).Clicked() {
			a.saveAIKey()
		}
		if ui.Button(c.Key("ai-key-clear"), locales.AiKeyClear()).Disabled(a.ai.keyBusy || !a.ai.keyConfigured).Clicked() {
			a.clearAIKey()
		}
		if a.ai.keyBusy {
			ui.Text(c, locales.AiKeyBusy()).FontSize(12).TextColor(tMuted(c))
		}
	})
	if a.settingsUI.aiKeyErr != "" {
		ui.Text(c, a.settingsUI.aiKeyErr).FontSize(12).TextColor(c.Theme().Danger)
	}
	if a.ai.keyConfigured {
		ui.Text(c, locales.AiKeyConfigured()).FontSize(12).TextColor(tMuted(c))
	}
	// 隐私说明：明示数据流向，本地 Ollama 不出本机。
	ui.Text(c, locales.AiPrivacy()).FontSize(12).TextColor(tMuted(c)).Wrap()
}

// aiTestRow 连接测试：一条最小补全请求，行内展示结果。
func (a *app) aiTestRow(c *ui.Context) {
	settingsRow(c, locales.AiTest(), locales.AiTestHint(), func() {
		if ui.Button(c.Key("ai-test"), locales.AiTestButton()).Disabled(a.ai.testing).Clicked() {
			a.testAIConnection()
		}
	})
	if a.ai.testing {
		ui.Text(c, locales.AiTestRunning()).FontSize(12).TextColor(tMuted(c))
	} else if a.ai.testResult != "" {
		color := c.Theme().TextMuted
		if a.ai.testErr {
			color = c.Theme().Danger
		}
		ui.Text(c, a.ai.testResult).FontSize(12).TextColor(color).Wrap()
	}
}

// aiProviderOptions 提供方的当前语言显示名（顺序 = 预设表顺序）。
func aiProviderOptions() []string {
	out := make([]string, 0, len(ai.Providers))
	for _, p := range ai.Providers {
		out = append(out, aiProviderDisplay(p.ID))
	}
	return out
}

// aiProviderDisplay 提供方 ID 的当前语言显示名；空串（未选择）显示占位。
func aiProviderDisplay(id string) string {
	switch id {
	case "openai":
		return locales.AiProviderOpenAI()
	case "deepseek":
		return locales.AiProviderDeepSeek()
	case "openrouter":
		return locales.AiProviderOpenRouter()
	case "ollama":
		return locales.AiProviderOllama()
	case "custom":
		return locales.AiProviderCustom()
	}
	return locales.AiProviderCustom()
}

// aiProviderIDOfDisplay 显示名 → 提供方 ID；未知回退 custom。
func aiProviderIDOfDisplay(display string) string {
	for _, p := range ai.Providers {
		if aiProviderDisplay(p.ID) == display {
			return p.ID
		}
	}
	return "custom"
}
