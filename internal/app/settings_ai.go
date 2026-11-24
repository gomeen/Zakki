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

			a.aiProviderRow(c)
			a.aiEndpointRow(c)
			a.aiKeyRow(c)
			a.aiModelRow(c)
			a.aiTestRow(c)

			sectionDivider(c)
			settingsSectionTitle(c, locales.AiBehaviorTitle())
			a.aiContextRow(c)
			if a.settingsUI.keyErr != "" {
				ui.Text(c, a.settingsUI.keyErr).FontSize(12).TextColor(c.Theme().Danger).Wrap()
			}

			ui.Box(c).Grow(1)
			a.cardResetFooter(c, "ai", pageAI)
		})
	})
}

// aiEnableRow 开启时同时提交用户尚未回车的连接配置。
func (a *app) aiEnableRow(c *ui.Context) {
	settingsRow(c, locales.AiEnable(), locales.AiEnableHint(), func() {
		if ui.Switch(c.Key("ai-enabled"), &a.settingsUI.draft.AI.Enabled).Label(locales.AiEnable()).Changed() {
			on := a.settingsUI.draft.AI.Enabled
			// 开启时顺带提交输入框里尚未回车的端点/模型草稿：避免用户填完
			// 直接开开关被校验拒绝、助理条不弹出的困扰。
			if err := a.updatePreferences(func(p *preferences) {
				p.AI.Enabled = on
				if on {
					p.AI.BaseURL = strings.TrimSpace(a.settingsUI.aiBaseURLText)
					p.AI.Model = strings.TrimSpace(a.settingsUI.aiModelText)
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
	ui.Text(c, locales.AiPrivacy()).FontSize(12).TextColor(tMuted(c)).Wrap()
}

// aiProviderRow 提供方预设：切换预填端点（custom 保留用户输入）。
func (a *app) aiProviderRow(c *ui.Context) {
	settingsRow(c, locales.AiProvider(), locales.AiProviderHint(), func() {
		c.Key("ai-provider")
		if boundedSelect(c, &a.settingsUI.aiProviderSel, aiProviderOptions()).Width(240).Changed() {
			a.ai.invalidateConnection()
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
		input := ui.TextInput(c.Key("ai-baseurl"), &a.settingsUI.aiBaseURLText).Width(240).
			Placeholder("https://api.openai.com/v1")
		if input.Changed() {
			a.ai.invalidateConnection()
		}
		if input.Submitted() {
			v := strings.TrimSpace(a.settingsUI.aiBaseURLText)
			a.settingsUI.draft.AI.BaseURL = v
			a.pushDraft(func(p *preferences) { p.AI.BaseURL = v })
		}
	})
}

// aiModelRow 保持模型选择与二级操作同行，状态只占一行说明。
func (a *app) aiModelRow(c *ui.Context) {
	models := a.currentAIModels()
	hint := locales.AiModelHint()
	modelErr := ""
	if a.ai.modelsSource == a.aiSettingsSource() {
		modelErr = a.ai.modelsErr
	}
	switch {
	case a.ai.listing:
		hint = locales.AiModelsFetching()
	case modelErr != "":
		hint = ""
	case len(models) > 0 && !a.ai.manualModel:
		hint = locales.AiModelsLoaded(strconv.Itoa(len(models)))
	}
	settingsRow(c, locales.AiModel(), hint, func() {
		ui.Row(c.Key("ai-model-controls")).AlignItems(ui.Center).Gap(6).Children(func() {
			const fieldWidth = 202
			if len(models) > 0 && !a.ai.manualModel {
				c.Key("ai-model-select")
				if boundedSelectWithPlaceholder(c, &a.settingsUI.aiModelText, models, locales.AiModelPlaceholder()).Width(fieldWidth).Changed() {
					a.saveAIModel()
				}
			} else {
				input := ui.TextInput(c.Key("ai-model"), &a.settingsUI.aiModelText).Width(fieldWidth).
					Label(locales.AiModel()).Placeholder("gpt-4o-mini")
				if input.Changed() {
					// 加载完成时不能打断已经开始的手动输入。
					a.ai.manualModel = true
					a.ai.invalidateTest()
				}
				if input.Submitted() {
					a.saveAIModel()
				}
			}
			a.aiModelMenu(c, len(models) > 0)
		})
	})
	if modelErr != "" {
		ui.Text(c, modelErr).FontSize(12).TextColor(c.Theme().Danger).Wrap()
	}
}

func (a *app) aiModelMenu(c *ui.Context, hasModels bool) {
	label := locales.AiModelActions()
	btn := ui.ButtonBase(c.Key("ai-model-actions")).Label(label).Tooltip(label).
		Size(32, 32).Radius(c.Theme().Radius).AlignItems(ui.Center).Justify(ui.Center)
	if btn.Hovered() || btn.Pressed() {
		btn.Background(c.Theme().SurfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, iconEllipsis).Size(16, 16).TextColor(tMuted(c))
	})
	btn.Menu(func(m *ui.Menu) {
		if m.Item(locales.AiModelsFetch()).Disabled(a.ai.listing || a.ai.keyBusy).Chosen() {
			a.fetchAIModels()
		}
		if hasModels {
			mode := locales.AiModelManual()
			if a.ai.manualModel {
				mode = locales.AiModelChoose()
			}
			if m.Item(mode).Chosen() {
				a.ai.manualModel = !a.ai.manualModel
			}
		}
	})
}

func (a *app) saveAIModel() {
	a.ai.invalidateTest()
	baseURL := a.aiSettingsSource().baseURL
	model := strings.TrimSpace(a.settingsUI.aiModelText)
	if err := a.updatePreferences(func(p *preferences) {
		p.AI.BaseURL, p.AI.Model = baseURL, model
	}); err != nil {
		a.settingsUI.keyErr = err.Error()
		return
	}
	a.settingsUI.aiBaseURLText, a.settingsUI.aiModelText = baseURL, model
	a.settingsUI.keyErr = ""
}

// aiKeyRow 已存密钥不回显，回车提交新密钥，清除动作留在输入框菜单。
func (a *app) aiKeyRow(c *ui.Context) {
	placeholder, hint := "sk-…", locales.AiKeyHint()
	if a.ai.keyConfigured {
		placeholder, hint = "••••••••", locales.AiKeyConfigured()
	}
	if a.ai.keyBusy {
		hint = locales.AiKeyBusy()
	} else if a.settingsUI.aiKeyErr != "" {
		hint = ""
	}
	settingsRow(c, locales.AiKeyTitle(), hint, func() {
		input := ui.TextInput(c.Key("ai-key"), &a.settingsUI.aiKeyText).Width(240).
			Label(locales.AiKeyLabel()).Placeholder(placeholder).Password().Disabled(a.ai.keyBusy)
		if input.Submitted() {
			a.saveAIKey()
		}
		if a.ai.keyConfigured {
			input.ContextMenu(func(m *ui.Menu) {
				m.EditItems()
				m.Separator()
				if m.Item(locales.AiKeyClear()).Disabled(a.ai.keyBusy).Chosen() {
					a.clearAIKey()
				}
			})
		}
	})
	if a.settingsUI.aiKeyErr != "" {
		ui.Text(c, a.settingsUI.aiKeyErr).FontSize(12).TextColor(c.Theme().Danger).Wrap()
	}
}

// aiTestRow 连接测试：一条最小补全请求，行内展示结果。
func (a *app) aiTestRow(c *ui.Context) {
	settingsRow(c, locales.AiTest(), locales.AiTestHint(), func() {
		if ui.Button(c.Key("ai-test"), locales.AiTestButton()).Disabled(a.ai.testing || a.ai.keyBusy).Clicked() {
			a.testAIConnection()
		}
	})
	if a.ai.testing {
		ui.Text(c, locales.AiTestRunning()).FontSize(12).TextColor(tMuted(c))
	} else if a.ai.testResult != "" && a.ai.testSource == a.aiSettingsSource() &&
		a.ai.testModel == strings.TrimSpace(a.settingsUI.aiModelText) {
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
