package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"zakki/locales"
)

func (a *app) onboardingView(c *ui.Context) {
	ob := &a.onboarding
	dark := themeIsDark(c.Theme())
	if ob.appearance.AppearanceMode == "light" {
		dark = false
	}
	if ob.appearance.AppearanceMode == "dark" {
		dark = true
	}
	pal := colorPaletteFor(ob.appearance.EffectiveColorTheme(), -1, dark)
	t := *c.Theme()
	t.Background, t.Text, t.TextMuted = pal.background, pal.text, pal.textMuted
	t.Surface, t.Border, t.Accent = pal.surface, pal.border, pal.accent
	t.Font, t.FontSize = bodyFontStack("pingfang", ""), 13
	c.SetTheme(&t)
	ui.Column(c).Fill().Padding(18, 32, 24, 32).Gap(16).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			label := locales.OnboardStep(ob.step, onboardingLastStep)
			switch ob.phase {
			case "preparing":
				label = locales.FontPrepareTitle()
			case "ready":
				label = locales.OnboardReady()
			case "saving":
				label = locales.OnboardSaving()
			}
			ui.Text(c, label).FontSize(11).TextColor(tMuted(c)).Grow(1)
			if ob.phase == "" {
				for i := 1; i <= onboardingLastStep; i++ {
					ink := t.Border
					if i == ob.step {
						ink = t.Accent
					}
					ui.Box(c.Key("ob-dot-"+strconv.Itoa(i))).Size(5, 5).Radius(999).Background(ink)
				}
			}
		})
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				switch ob.phase {
				case "preparing":
					ui.Text(c, locales.FontPrepareTitle()).FontSize(23).Bold()
					fontProgressView(c, &ob.preparation)
				case "ready", "saving":
					ui.Column(c).FillWidth().Height(290).Center().Gap(16).Children(func() {
						if ob.phase == "ready" {
							ui.Text(c, "✓").FontSize(30)
							ui.Text(c, locales.OnboardReady()).FontSize(23).Bold()
						} else {
							ui.Text(c, locales.OnboardSaving()).FontSize(23).Bold()
						}
					})
				default:
					if ob.step == 1 {
						a.obAppearance(c)
					} else {
						a.obNotes(c)
					}
				}
				if ob.dirErr != "" {
					ui.Text(c, ob.dirErr).FontSize(12).TextColor(t.Danger)
				}
			})
		})
		a.obFooter(c)
	})
}

// filledSegmented 是等分轨道的分段选择。ui.Segmented 按文字宽度排段，
// 轨道被拉到固定宽度时选中底只包住文案，右侧留下空轨道。
func filledSegmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	parts := ui.SegmentedBase(c, selected, len(labels))
	track := parts.Track.FillWidth().AlignItems(ui.Stretch).
		Padding(t.Space(0.5)).Gap(t.Space(0.5)).
		Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	track.Children(func() {
		for i, label := range labels {
			seg := parts.Segment(i).Grow(1).
				Padding(t.Space(1), t.Space(3)).
				Radius(segmentInnerRadius(t)).
				TextColor(t.Text)
			switch {
			case i == *selected:
				seg.Background(segmentSelectedColor(t)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
			case seg.Pressed():
				seg.Background(t.SurfacePressed)
			case seg.Hovered():
				seg.Background(t.SurfaceHover)
			default:
				seg.Background(ui.Transparent)
			}
			seg.Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return track
}

func segmentInnerRadius(t *ui.Theme) float32 {
	r := t.Radius - t.Space(0.5)
	if r < 0 {
		return 0
	}
	return r
}

// segmentSelectedColor 与 ui.Segmented 的浅色选中底一致：浅色用页面背景，深色用按下表面。
func segmentSelectedColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.SurfacePressed
	}
	return t.Background
}

func obRow(c *ui.Context, label string, control func()) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
		ui.Text(c, label).Grow(1)
		ui.Column(c).Width(244).Children(control)
	})
}

// obWideRow 与 obRow 相同，但控件占满剩余宽度：语言分段有四种语言，
// 固定在 244pt 会把段内文字裁掉。
func obWideRow(c *ui.Context, label string, control func()) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
		ui.Text(c, label)
		ui.Column(c).Grow(1).Children(control)
	})
}

func (a *app) obAppearance(c *ui.Context) {
	ob := &a.onboarding
	ui.Text(c, locales.OnboardAppearanceTitle()).FontSize(23).Bold()
	ui.Column(c).FillWidth().Gap(14).Padding(12, 0, 0, 0).Children(func() {
		obWideRow(c, locales.LanguageRow(), func() {
			displays, tags := languageSegments()
			filledSegmented(c.Key("ob-language"), &ob.languageIndex, displays...).Label(locales.LanguageRow()).OnChange(func() {
				if ob.languageIndex < 0 || ob.languageIndex >= len(tags) {
					return
				}
				tag := tags[ob.languageIndex]
				ob.appearance.Language = tag
				if err := a.updatePreferences(func(p *preferences) { p.Appearance.Language = tag }); err != nil {
					ob.dirErr = locales.OnboardSaveErr(err.Error())
				}
			})
		})
		obRow(c, locales.OnboardMode(), func() {
			filledSegmented(c.Key("ob-mode"), &ob.modeIndex, locales.ModeFollowSystem(), locales.ModeLight(), locales.ModeDark()).Label(locales.OnboardMode()).OnChange(func() {
				ob.appearance.AppearanceMode = []string{"system", "light", "dark"}[ob.modeIndex]
			})
		})
		obRow(c, locales.OnboardStyle(), func() {
			filledSegmented(c.Key("ob-style"), &ob.styleIndex, locales.StyleStandard(), locales.StyleSketch()).Label(locales.OnboardStyle()).OnChange(func() {
				ob.appearance.Style = []string{styleStandard, styleSketch}[ob.styleIndex]
			})
		})
		obRow(c, locales.FontChinese(), func() { a.fontPicker(c.Key("ob-cjk"), &ob.appearance.BodyFont, 0, locales.FontChinese(), 244) })
		obRow(c, locales.FontWestern(), func() { a.fontPicker(c.Key("ob-latin"), &ob.appearance.LatinFont, 1, locales.FontWestern(), 244) })
		if !a.fonts.loaded && a.ready {
			ui.Text(c, locales.FontScanning()).FontSize(11).TextColor(tMuted(c))
		}
	})
}

func (a *app) obNotes(c *ui.Context) {
	ob := &a.onboarding
	ui.Text(c, locales.OnboardNotesTitle()).FontSize(23).Bold()
	ui.Column(c).FillWidth().Gap(28).Padding(12, 0, 0, 0).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, locales.OnboardTabCountTitle()).Grow(1)
			ui.Text(c, locales.OnboardTabRange(MaxTabCount)).FontSize(12).TextColor(tMuted(c))
			// 按钮默认左右内边距是 14pt，28pt 宽时符号会被挤到右侧；数字框按文字左对齐，
			// 而且比按钮更高。三格同高，符号在格子里居中。
			ui.Button(c.Key("ob-minus"), "−").Size(28, 28).Padding(0).Center().
				Label(locales.OnboardFewerTabs()).Disabled(ob.tabCount <= 1).OnClick(func() { a.setTabCount(ob.tabCount - 1) })
			ui.TextInput(c.Key("ob-count"), &ob.tabCountText).Size(40, 28).Padding(0, 4).TextAlign(ui.Center).
				Label(locales.OnboardTabCountTitle()).OnChange(a.commitTabCountText)
			ui.Button(c.Key("ob-plus"), "+").Size(28, 28).Padding(0).Center().
				Label(locales.OnboardMoreTabs()).Disabled(ob.tabCount >= MaxTabCount).OnClick(func() { a.setTabCount(ob.tabCount + 1) })
		})
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, locales.StorageChooseTitle()).Bold()
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(14).Children(func() {
				ui.Text(c, ob.dirDraft).FontSize(12).Grow(1)
				ui.Button(c, locales.CommonChange()).OnClick(a.obPickFolder)
			})
		})
	})
}

func (a *app) obFooter(c *ui.Context) {
	ob := &a.onboarding
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		switch ob.phase {
		case "preparing":
			ui.Button(c, locales.FontEditChoices()).OnClick(a.editOnboarding)
			ui.Box(c).Grow(1)
			if ob.preparation.err != "" {
				ui.Button(c, locales.FontUseSystem()).OnClick(a.useSystemOnboardingFonts)
				ui.PrimaryButton(c, locales.FontRetry()).OnClick(a.submitOnboarding)
			} else {
				ui.PrimaryButton(c, locales.FontPreparing()).Disabled(true)
			}
		case "ready":
			ui.Button(c, locales.FontEditChoices()).OnClick(a.editOnboarding)
			ui.Box(c).Grow(1)
			ui.PrimaryButton(c, locales.OnboardStartUsing()).OnClick(func() { a.finishOnboarding(true) })
		case "saving":
			ui.Box(c).Grow(1)
			ui.PrimaryButton(c, locales.OnboardSaving()).Disabled(true)
		default:
			if ob.step > 1 {
				ui.Button(c, locales.OnboardBack()).OnClick(func() { ob.step-- })
			}
			ui.Box(c).Grow(1)
			ui.PrimaryButton(c, a.obNextLabel()).Disabled(!a.fonts.loaded && a.ready).OnClick(func() {
				if ob.step == 1 {
					ob.step = 2
				} else {
					a.submitOnboarding()
				}
			})
		}
	})
}
