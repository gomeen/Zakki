package app

// 设置窗口「通用」页：常规分区（语言、窗口）与迁移状态；外观分区在 settings_ui.go。

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

// generalPage 通用页：常规（语言、窗口）在前，其后是外观各分区，整页一个滚动区，
// 底部单一「恢复本页默认值」同时恢复窗口与外观。
func (a *app) generalPage(c *ui.Context) {
	ui.Column(c).Fill().Gap(16).Children(func() {
		a.migrationStatus(c)
		ui.Scroll(c.Key("general-controls")).Fill().Grow(1).ScrollbarInsets(0, 12, 0, 0).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 22, 0, 0).Children(func() {
				settingsSectionTitle(c, locales.CardGeneral())
				a.languageRow(c)
				settingsRow(c, locales.RememberBounds(), "", func() {
					if ui.Switch(c, &a.settingsUI.draft.Window.RememberBounds).Label(locales.RememberBoundsSwitch()).Changed() {
						a.pushWindow()
					}
				})
				sectionDivider(c)
				a.appearanceSections(c)
				a.cardResetFooter(c, "general", pageGeneral)
			})
		})
	})
}

// languageOptions 语言选择项：跟随系统 + 各目录语言（以语言自称显示）。
// 顺序固定为拉丁 → 日文 → 中文，设置下拉与向导分段共用。
func languageOptions() []struct{ tag, display string } {
	return []struct{ tag, display string }{
		{"", locales.MenuFollowSystem()},
		{"en", i18n.Autonym("en")},
		{"ja", i18n.Autonym("ja")},
		{"zh-Hans", i18n.Autonym("zh-Hans")},
		{"zh-Hant", i18n.Autonym("zh-Hant")},
	}
}

// languageSegments 首启向导的语言分段：只有具体语言，不含「跟随系统」。
// 返回各段显示名与下标对应的语言 tag，两段切片等长且下标对齐。
func languageSegments() (displays, tags []string) {
	for _, o := range languageOptions() {
		if o.tag == "" {
			continue
		}
		displays = append(displays, o.display)
		tags = append(tags, o.tag)
	}
	return displays, tags
}

// languageSegmentIndex 返回 tag 在分段里的下标；未知 tag 回落到第一段（en）。
func languageSegmentIndex(tags []string, tag string) int {
	for i, t := range tags {
		if t == tag {
			return i
		}
	}
	return 0
}

func languageDisplay(tag string) string {
	for _, o := range languageOptions() {
		if o.tag == tag {
			return o.display
		}
	}
	return languageOptions()[0].display
}

func languageTagOfDisplay(display string) string {
	for _, o := range languageOptions() {
		if o.display == display {
			return o.tag
		}
	}
	return ""
}

// languageRow 语言选择：变更经 updatePreferences 即时生效并持久化。
// 跟随系统时在行下补一行实际生效的语言，避免用户分不清选中的是「跟随系统」
// 还是系统解析出的具体语言。
func (a *app) languageRow(c *ui.Context) {
	opts := languageOptions()
	displays := make([]string, len(opts))
	for i, o := range opts {
		displays[i] = o.display
	}
	detail := ""
	if a.prefs.Appearance.Language == "" {
		detail = locales.LanguageRowFollowing(i18n.Autonym(mygo.App.Locale()))
	}
	settingsRow(c, locales.LanguageRow(), detail, func() {
		if boundedSelect(c, &a.settingsUI.languageSel, displays).Width(200).Changed() {
			tag := languageTagOfDisplay(a.settingsUI.languageSel)
			a.settingsUI.draft.Appearance.Language = tag
			_ = a.updatePreferences(func(p *preferences) { p.Appearance.Language = tag })
		}
	})
}

// migrationStatus 偏好文件异常的恢复入口；全新安装不再有旧配置导入。
func (a *app) migrationStatus(c *ui.Context) {
	if a.currentPrefsIssue().Kind == prefsIssuePrefsBroken {
		ui.Column(c).FillWidth().Gap(6).Padding(10, 12).Radius(8).
			Background(c.Theme().Surface).BorderWidth(1).BorderColor(c.Theme().Danger).Children(func() {
			ui.Text(c, locales.PrefsNeedRecovery()).Bold()
			ui.Text(c, a.currentPrefsIssue().Message).FontSize(12).TextColor(tMuted(c))
			if ui.Button(c, locales.DiscardAndResetPrefs()).Clicked() {
				if err := a.confirmRecoverPreferences(); err != nil {
					a.settingsUI.keyErr = err.Error()
				}
				a.settingsUI.syncDraftFromPrefs(a.prefs)
			}
		})
	}
}
