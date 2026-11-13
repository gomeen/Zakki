package app

// 设置页布局与预览样式的测试：previewStyle 的覆盖/跟随语义，
// 以及新版卡片式设置页的关键内容渲染。

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

func TestResolveStyleAppliesOverrides(t *testing.T) {
	a := newTestApp(t)
	a.prefs.Appearance.MarkdownTheme = "book"
	a.prefs.Appearance.ColorTheme = "paper"
	a.prefs.Appearance.BodyFont = "songti"
	a.prefs.Appearance.MonoFont = "menlo"
	a.prefs.Appearance.BodySize = 21
	a.prefs.Appearance.LineHeight = 2.0

	s := a.resolveStyle(-1, false, true)
	if s.bodySize != 21 || s.lineHeight != 2.0 {
		t.Fatalf("排版微调应作用于样式: size=%v lh=%v", s.bodySize, s.lineHeight)
	}
	if s.bodyFont != bodyFontStack("songti", a.prefs.Appearance.LatinFont) || s.monoFont != monoFontStack("menlo") {
		t.Fatalf("字体覆盖应作用于样式: %q %q", s.bodyFont, s.monoFont)
	}
	if s.headingSizes[1] != roundHalf(21*markdownLayoutByID("book").headingScale[1]) {
		t.Fatalf("标题字号应随字号覆盖缩放: %+v", s.headingSizes)
	}
	if s.background != colorThemes[0].light.background { // paper
		t.Fatalf("颜色主题应作用于样式: %v", s.background)
	}
}

func TestResolveStyleFollowsLayoutWithoutOverrides(t *testing.T) {
	a := newTestApp(t)
	a.prefs.Appearance.MarkdownTheme = "book"
	a.prefs.Appearance.BodySize, a.prefs.Appearance.LineHeight = 0, 0
	a.prefs.Appearance.BodyFont, a.prefs.Appearance.LatinFont, a.prefs.Appearance.MonoFont = "", "", ""

	layout := markdownLayoutByID("book")
	s := a.resolveStyle(-1, false, true)
	if s.bodySize != layout.bodySize || s.lineHeight != layout.lineHeight {
		t.Fatalf("空覆盖应跟随排版主题: %+v", s)
	}
	// 字体与排版主题解耦：book 主题不再带宋体，空键始终是默认栈。
	if s.bodyFont != sonnetBodyFont || s.monoFont != sonnetMonoFont {
		t.Fatalf("空字体键应为默认栈: %q %q", s.bodyFont, s.monoFont)
	}
}

func TestSettingsViewCardLayout(t *testing.T) {
	a := newTestApp(t)
	a.winOpen = true

	a.settingsPage = pageGeneral
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 620)
	tt.Frame()
	for _, want := range []string{
		"界面外观", "风格", "颜色主题",
		"排版主题", "中文字体", "英文字体", "代码字体",
		"主题", "排版", "语法配色", "恢复本页默认值",
		"左右内边距", "上下内边距",
	} {
		if !tt.HasText(want) {
			t.Fatalf("外观页缺少 %q: %v", want, tt.Texts())
		}
	}
	// 页面级常驻预览已移除：不应再出现「预览」栏标题。
	if tt.HasText("预览") {
		t.Fatalf("外观页不应再有页面级预览栏: %v", tt.Texts())
	}

	a.settingsPage = pageStorage
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt = ui.NewTester(a.settingsView, 900, 620)
	tt.Frame()
	if !tt.HasText("笔记文件") || !tt.HasText(a.notes.DataPath()) {
		t.Fatalf("保存页缺少文件路径: %v", tt.Texts())
	}
	if tt.HasText("立即备份") || tt.HasText("备份文件夹") {
		t.Fatalf("保存页不应再有备份概念: %v", tt.Texts())
	}
}

// TestLanguageRowShowsEffectiveLanguage 跟随系统时在语言行下补出实际生效的语言，
// 显式选择语言后不再重复说明（下拉里已经显示具体语言）。
func TestLanguageRowShowsEffectiveLanguage(t *testing.T) {
	a := newTestApp(t)
	a.winOpen = true
	a.settingsPage = pageGeneral
	a.prefs.Appearance.Language = ""
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	following := func() string {
		return locales.LanguageRowFollowing(i18n.Autonym(mygo.App.Locale()))
	}
	tt := ui.NewTester(a.settingsView, 900, 620)
	tt.Frame()
	if !tt.HasText(following()) {
		t.Fatalf("跟随系统应显示实际生效语言 %q: %v", following(), tt.Texts())
	}

	a.prefs.Appearance.Language = "en"
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt.Frame()
	if tt.HasText(following()) {
		t.Fatalf("显式选择语言后不应再出现跟随系统的说明: %v", tt.Texts())
	}
}

func TestSettingsWideLayoutFallback(t *testing.T) {
	a := newTestApp(t)
	a.winOpen = true
	a.settingsPage = pageGeneral
	a.settingsUI.syncDraftFromPrefs(a.prefs)

	// 窄视口：单栏滚动列，内容完整且无页面级预览。
	tt := ui.NewTester(a.settingsView, 800, 620)
	tt.Frame()
	for _, want := range []string{"颜色主题", "排版主题", "中文字体"} {
		if !tt.HasText(want) {
			t.Fatalf("窄视口单栏缺少 %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("预览") {
		t.Fatalf("窄视口不应出现页面级预览栏: %v", tt.Texts())
	}
	if err := tt.Click("经典"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("石墨"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Appearance.ColorTheme != "graphite" {
		t.Fatalf("窄视口下主题下拉应可点击生效: %+v", a.prefs.Appearance)
	}
}

// TestSettingsFontSplitPersists 排版微调里中西文字体分开设置：分别选中后各自落到
// 偏好并持久化，中文选择不受西文选择影响。
func TestSettingsFontSplitPersists(t *testing.T) {
	a := newTestApp(t)
	if err := a.loadPreferences(); err != nil {
		t.Fatal(err)
	}
	a.settingsPage = pageGeneral
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	// 视口要够高，排版微调卡片与下拉选项才在可视区内。
	tt := ui.NewTester(a.settingsView, 900, 1200)
	tt.Frame()
	latin := a.prefs.Appearance.LatinFont

	// 下拉：先点开当前值，再点选项。
	if err := tt.Click("霞鹜文楷（默认）"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("宋体"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Appearance.BodyFont != "songti" {
		t.Fatalf("中文字体应即时生效: %+v", a.prefs.Appearance)
	}
	if a.prefs.Appearance.LatinFont != latin {
		t.Fatalf("选中文字体不应改动西文设置: %q", a.prefs.Appearance.LatinFont)
	}
	// 英文字体行在代码字体行之前：同名显示时先点到的是英文字体下拉。
	if err := tt.Click(fontDisplayOf(1, latin)); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Noteworthy"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Appearance.LatinFont != "noteworthy" {
		t.Fatalf("西文字体应即时生效: %+v", a.prefs.Appearance)
	}
	if a.prefs.Appearance.BodyFont != "songti" {
		t.Fatalf("选西文字体不应改动中文设置: %q", a.prefs.Appearance.BodyFont)
	}
	// 解析后的样式栈：西文在前，中文仍在。
	style := a.resolveStyle(-1, false, true)
	if !strings.HasPrefix(style.bodyFont, "Noteworthy, ") || !strings.Contains(style.bodyFont, "Songti SC") {
		t.Fatalf("正文栈应为西文在前中文在后: %q", style.bodyFont)
	}
	// 持久化。
	a.shutdownPreferences()
	saved, err := decodePreferences(prefsReadRaw(t, a.preferencesPath()))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Appearance.BodyFont != "songti" || saved.Appearance.LatinFont != "noteworthy" {
		t.Fatalf("中西文字体应分别落盘: %+v", saved.Appearance)
	}
}

func TestSettingsDraftMergesExternalUpdatesByField(t *testing.T) {
	a := newTestApp(t)
	a.settingsUI.syncDraftFromPrefs(a.prefs)

	// 非模态窗口打开期间，其他入口更新同一 section 的另一个字段。
	if err := a.updatePreferences(func(p *preferences) {
		p.Window.AlwaysOnTop = true
		p.Appearance.Language = "en"
	}); err != nil {
		t.Fatal(err)
	}
	if !a.settingsUI.draft.Window.AlwaysOnTop || a.settingsUI.draft.Appearance.Language != "en" {
		t.Fatalf("未编辑字段应同步外部更新: %+v %+v", a.settingsUI.draft.Window, a.settingsUI.draft.Appearance)
	}

	// 用户随后只编辑 RememberBounds 和 ColorTheme，不得用陈旧草稿覆盖外部值。
	a.settingsUI.draft.Window.RememberBounds = false
	a.pushWindow()
	a.settingsUI.draft.Appearance.ColorTheme = "paper"
	a.pushAppearance()
	if !a.prefs.Window.AlwaysOnTop || a.prefs.Window.RememberBounds {
		t.Fatalf("窗口字段级合并失败: %+v", a.prefs.Window)
	}
	if a.prefs.Appearance.Language != "en" || a.prefs.Appearance.ColorTheme != "paper" {
		t.Fatalf("外观字段级合并失败: %+v", a.prefs.Appearance)
	}
}

func TestSettingsInvalidNumericTextAndErrorSurviveFrames(t *testing.T) {
	a := newTestApp(t)
	a.settingsPage = pageGeneral
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	a.settingsUI.bodySizeText = "99"
	a.commitFloatField("bodySize")
	if a.settingsUI.bodySizeErr == "" || !a.settingsUI.bodySizeDirty {
		t.Fatalf("越界值必须保持 dirty 与行内错误: %+v", a.settingsUI)
	}
	if a.prefs.Appearance.BodySize != 0 {
		t.Fatalf("越界值不得提交: %v", a.prefs.Appearance.BodySize)
	}

	tt := ui.NewTester(a.settingsView, 900, 1200)
	for frame := 1; frame <= 2; frame++ {
		tt.Frame()
		if a.settingsUI.bodySizeText != "99" || a.settingsUI.bodySizeErr == "" || !a.settingsUI.bodySizeDirty {
			t.Fatalf("第 %d 帧清掉了非法输入: text=%q err=%q dirty=%v", frame,
				a.settingsUI.bodySizeText, a.settingsUI.bodySizeErr, a.settingsUI.bodySizeDirty)
		}
	}

	if err := a.updatePreferences(func(p *preferences) { p.Appearance.ColorTheme = "paper" }); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsUI.bodySizeText != "99" || a.settingsUI.bodySizeErr == "" || !a.settingsUI.bodySizeDirty {
		t.Fatalf("外部更新不得清掉非法数字草稿: text=%q err=%q dirty=%v",
			a.settingsUI.bodySizeText, a.settingsUI.bodySizeErr, a.settingsUI.bodySizeDirty)
	}

	a.settingsUI.bodySizeText = "18"
	a.commitFloatField("bodySize")
	tt.Frame()
	if a.settingsUI.bodySizeDirty || a.settingsUI.bodySizeErr != "" {
		t.Fatalf("合法修正后才应清 dirty/error: err=%q dirty=%v", a.settingsUI.bodySizeErr, a.settingsUI.bodySizeDirty)
	}
	if a.prefs.Appearance.BodySize != 18 || a.settingsUI.bodySizeText != "18" {
		t.Fatalf("合法修正未提交或未规范化: prefs=%v text=%q", a.prefs.Appearance.BodySize, a.settingsUI.bodySizeText)
	}
}

func TestSettingsGeneralResetScope(t *testing.T) {
	a := newTestApp(t)
	a.openSettings(pageGeneral)
	if err := a.updatePreferences(func(p *preferences) {
		p.Window.RememberBounds = false
		p.Appearance.ColorTheme = "graphite"
		p.Appearance.BodySize = 22
		p.Advanced.WindowColumns = 30
		p.Advanced.WindowRows = 20
		p.Storage.SaveIntervalMS = 5000
		p.Git.AutoCommit = true
		p.Keymap.Overrides["format.bold"] = "Alt+Shift+B"
	}); err != nil {
		t.Fatal(err)
	}
	before := a.prefs
	text := a.tabs[0].Text
	a.resetPageDefaults(pageGeneral)
	defaults := defaultPreferences()
	if a.prefs.Window != defaults.Window || a.prefs.Appearance != defaults.Appearance ||
		a.prefs.Advanced.WindowColumns != defaults.Advanced.WindowColumns || a.prefs.Advanced.WindowRows != defaults.Advanced.WindowRows {
		t.Fatalf("通用页重置未同时恢复窗口和外观: %+v", a.prefs)
	}
	if a.prefs.Storage != before.Storage || a.prefs.Git != before.Git || a.prefs.Keymap.Overrides["format.bold"] != "Alt+Shift+B" ||
		a.tabs[0].Text != text {
		t.Fatal("通用页重置不应影响存储、Git、快捷键或笔记内容")
	}
	if a.settingsUI.draft.Appearance != a.prefs.Appearance || a.settingsUI.draft.Window != a.prefs.Window {
		t.Fatal("通用页重置后草稿未同步")
	}
}

func TestResetPageAfterConfirmationDropsWhenWindowClosed(t *testing.T) {
	a := newTestApp(t)
	a.prefs.Window.AlwaysOnTop = true

	a.resetPageAfterConfirmation(pageGeneral)

	if !a.prefs.Window.AlwaysOnTop {
		t.Fatal("后台对话框结果不得在没有存活窗口时直接修改偏好")
	}
}

// TestSettingsCodeThemePersists 代码配色下拉：从默认柔和改选明快，落到偏好并持久化；
// 显式「跟随文本」也能选中并原样落盘（空串不可被 omitempty 吞掉）。
func TestSettingsCodeThemePersists(t *testing.T) {
	a := newTestApp(t)
	if err := a.loadPreferences(); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Appearance.CodeTheme != "soft" {
		t.Fatalf("新安装默认应为柔和: %+v", a.prefs.Appearance)
	}
	a.settingsPage = pageGeneral
	a.settingsUI.syncDraftFromPrefs(a.prefs)
	tt := ui.NewTester(a.settingsView, 900, 1200)
	tt.Frame()

	if err := tt.Click(locales.CodeThemeSoft()); err != nil {
		t.Fatalf("当前值应可见: %v", err)
	}
	if err := tt.Click(locales.CodeThemeVivid()); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Appearance.CodeTheme != "vivid" {
		t.Fatalf("代码配色应即时生效: %+v", a.prefs.Appearance)
	}
	// 显式选「跟随文本」：空串必须即时生效且落盘保留。
	if err := tt.Click(locales.CodeThemeVivid()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.CodeThemeNone()); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Appearance.CodeTheme != "" {
		t.Fatalf("跟随文本应即时生效: %+v", a.prefs.Appearance)
	}
	a.shutdownPreferences()
	saved, err := decodePreferences(prefsReadRaw(t, a.preferencesPath()))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Appearance.CodeTheme != "" {
		t.Fatalf("跟随文本应已落盘为空串: %+v", saved.Appearance)
	}
}

// TestSettingsPaddingFieldsCommit 内边距字段：非法与越界值不提交并保留行内错误，
// 合法值即时生效落盘，清空 = 跟随排版主题；解析后的样式与窗口换算同步吃覆盖。
func TestSettingsPaddingFieldsCommit(t *testing.T) {
	a := newTestApp(t)
	a.settingsPage = pageGeneral
	a.settingsUI.syncDraftFromPrefs(a.prefs)

	// 越界值：保持 dirty 与行内错误，不得提交。
	a.settingsUI.padXText = "4"
	a.commitFloatField("padX")
	if a.settingsUI.padXErr == "" || !a.settingsUI.padXDirty {
		t.Fatalf("越界值必须保持 dirty 与行内错误: %+v", a.settingsUI)
	}
	if a.prefs.Appearance.PadX != 0 {
		t.Fatalf("越界值不得提交: %v", a.prefs.Appearance.PadX)
	}
	// 非法数字：同样不提交。
	a.settingsUI.padYText = "abc"
	a.commitFloatField("padY")
	if a.settingsUI.padYErr == "" {
		t.Fatal("非法数字必须显示行内错误")
	}

	// 合法值提交并清 dirty/error。
	a.settingsUI.padXText = "32"
	a.commitFloatField("padX")
	if a.prefs.Appearance.PadX != 32 || a.settingsUI.padXErr != "" || a.settingsUI.padXDirty {
		t.Fatalf("合法值应提交: padX=%v err=%q dirty=%v", a.prefs.Appearance.PadX, a.settingsUI.padXErr, a.settingsUI.padXDirty)
	}
	a.settingsUI.padYText = "16"
	a.commitFloatField("padY")
	if a.prefs.Appearance.PadY != 16 {
		t.Fatalf("上下内边距应提交: %v", a.prefs.Appearance.PadY)
	}

	// 已提交左右 32 / 上下 16：样式应两者都覆盖。
	a.prefs.Appearance.MarkdownTheme = "book"
	s := a.resolveStyle(-1, false, true)
	if s.padX != 32 || s.padY != 16 {
		t.Fatalf("样式应合并覆盖与主题: padX=%v padY=%v", s.padX, s.padY)
	}
	// 清空上下后，上下应回到主题默认（book 主题上下 28）。
	a.settingsUI.padYText = ""
	a.commitFloatField("padY")
	s = a.resolveStyle(-1, false, true)
	if s.padX != 32 || s.padY != 28 {
		t.Fatalf("清空后上下应跟随主题: padX=%v padY=%v", s.padX, s.padY)
	}

	// 窗口尺寸换算同步吃覆盖：左右变宽同列数更宽，上下不动高度不变。
	w0, h0 := defaultWindowSize(a.prefs)
	a.prefs.Appearance.PadX = 60
	a.prefs.Appearance.PadY = 40
	w1, h1 := defaultWindowSize(a.prefs)
	if w1 <= w0 || h1 <= h0 {
		t.Fatalf("窗口尺寸应随内边距覆盖变大: %dx%d -> %dx%d", w0, h0, w1, h1)
	}

	// 清空 = 跟随主题；落盘含两个新键。逐个清：提交任一字段都会把另一个
	// 非 dirty 字段的文本重播为当前偏好值。
	a.settingsUI.padYText = ""
	a.commitFloatField("padY")
	a.settingsUI.padXText = ""
	a.commitFloatField("padX")
	if a.prefs.Appearance.PadX != 0 || a.prefs.Appearance.PadY != 0 {
		t.Fatalf("清空应跟随主题: %+v", a.prefs.Appearance)
	}
	a.shutdownPreferences()
	saved, err := decodePreferences(prefsReadRaw(t, a.preferencesPath()))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Appearance.PadX != 0 || saved.Version != currentPreferencesVersion {
		t.Fatalf("跟随语义应原样落盘: %+v", saved)
	}
}
