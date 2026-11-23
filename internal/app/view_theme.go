package app

// 双层主题：颜色主题（ColorTheme）与 Markdown 排版主题（MarkdownTheme）相互独立。
//
//   - ColorTheme 只提供语义颜色：背景、顶底栏、控件表面、正文/次级文字、
//     边框、强调、选择、代码背景等；不携带任何排版信息。
//   - MarkdownTheme 只提供排版：字体、基准字号、行高、标题比例、段落间距、
//     正文内边距以及引用/代码/表格的结构尺寸；不硬编码颜色。
//
// 最终样式 = 颜色主题 + 明暗模式 + Markdown 排版 + 用户排版覆盖。
// 经典颜色主题在主窗口继续随点配色；设置窗口使用经典主题的中性灰变体。
// 其余颜色主题使用固定背景，切点只改变导航圆点及当前点标识。

import (
	"math"

	"github.com/egoist/mygo/ui"

	"zakki/internal/fonts"

	"zakki/locales"
)

// ---- 颜色主题 ----

// colorPalette 是一套颜色主题在某个明暗模式下的语义颜色。
type colorPalette struct {
	background     ui.Color // 内容区背景
	header         ui.Color // 顶栏
	status         ui.Color // 底栏
	surface        ui.Color // 控件表面（输入框、代码底、浮层）
	border         ui.Color // 控件边界
	text           ui.Color // 正文文字
	textMuted      ui.Color // 次级文字
	accent         ui.Color // 强调（链接、勾选、焦点）
	selection      ui.Color // 选区叠色
	codeBackground ui.Color // 代码块底色
	quoteBorder    ui.Color // 引用竖条
	rule           ui.Color // 分隔线
}

type colorThemeDef struct {
	id    string
	light colorPalette
	dark  colorPalette
}

// colorThemeDisplayName 颜色主题的当前语言显示名；未知 ID 原样返回。
func colorThemeDisplayName(id string) string {
	switch id {
	case colorThemeClassic:
		return locales.ThemeClassic()
	case "paper":
		return locales.ThemePaper()
	case "warmSand":
		return locales.ThemeWarmSand()
	case "mistTeal":
		return locales.ThemeMistTeal()
	case "graphite":
		return locales.ThemeGraphite()
	case "sketch":
		return locales.ThemeSketch()
	case "claude":
		return locales.ThemeClaude()
	case "qoder":
		return locales.ThemeQoder()
	}
	return id
}

// colorThemes 是全部颜色主题；classic 之外的浅/深色底色成对给出，
// 正文/次级文字对实际背景 ≥ 4.5:1，必要边界 ≥ 3:1（有对比度测试保障）。
var colorThemes = []colorThemeDef{
	{
		id: "paper",
		light: colorPalette{
			background: ui.Hex("#fafaf8"), header: ui.Hex("#f0f0ec"), status: ui.Hex("#f0f0ec"),
			surface: ui.Hex("#ffffff"), border: ui.Hex("#dededa"),
			text: ui.Hex("#2a2c2e"), textMuted: ui.Hex("#6b6e72"),
			accent: ui.Hex("#3b6ea5"), selection: ui.RGBA(59, 110, 165, 0.28),
			codeBackground: ui.Hex("#f0f0ec"), quoteBorder: ui.Hex("#b9bcc2"), rule: ui.Hex("#dededa"),
		},
		dark: colorPalette{
			background: ui.Hex("#202224"), header: ui.Hex("#282a2d"), status: ui.Hex("#282a2d"),
			surface: ui.Hex("#2b2e31"), border: ui.Hex("#3d4045"),
			text: ui.Hex("#e8e9ea"), textMuted: ui.Hex("#a3a6a9"),
			accent: ui.Hex("#7fb3e3"), selection: ui.RGBA(127, 179, 227, 0.32),
			codeBackground: ui.Hex("#2b2e31"), quoteBorder: ui.Hex("#5a6068"), rule: ui.Hex("#3d4045"),
		},
	},
	{
		id: "warmSand",
		light: colorPalette{
			background: ui.Hex("#f5efe3"), header: ui.Hex("#eee6d5"), status: ui.Hex("#eee6d5"),
			surface: ui.Hex("#fdfaf3"), border: ui.Hex("#ddd2bc"),
			text: ui.Hex("#43331f"), textMuted: ui.Hex("#77664e"),
			accent: ui.Hex("#96551f"), selection: ui.RGBA(150, 85, 31, 0.26),
			codeBackground: ui.Hex("#ece3d0"), quoteBorder: ui.Hex("#c0ac8a"), rule: ui.Hex("#ddd2bc"),
		},
		dark: colorPalette{
			background: ui.Hex("#29251f"), header: ui.Hex("#312c25"), status: ui.Hex("#312c25"),
			surface: ui.Hex("#342f28"), border: ui.Hex("#4a443a"),
			text: ui.Hex("#efe7da"), textMuted: ui.Hex("#b3a893"),
			accent: ui.Hex("#e2ab67"), selection: ui.RGBA(226, 171, 103, 0.30),
			codeBackground: ui.Hex("#342f28"), quoteBorder: ui.Hex("#6e6350"), rule: ui.Hex("#4a443a"),
		},
	},
	{
		id: "mistTeal",
		light: colorPalette{
			background: ui.Hex("#edf4f1"), header: ui.Hex("#e2ece8"), status: ui.Hex("#e2ece8"),
			surface: ui.Hex("#f8fbfa"), border: ui.Hex("#cfdfd8"),
			text: ui.Hex("#1e3a33"), textMuted: ui.Hex("#55716a"),
			accent: ui.Hex("#24685a"), selection: ui.RGBA(36, 104, 90, 0.24),
			codeBackground: ui.Hex("#e1ebe7"), quoteBorder: ui.Hex("#9dbcb2"), rule: ui.Hex("#cfdfd8"),
		},
		dark: colorPalette{
			background: ui.Hex("#1d2927"), header: ui.Hex("#233130"), status: ui.Hex("#233130"),
			surface: ui.Hex("#263533"), border: ui.Hex("#394946"),
			text: ui.Hex("#e4eeea"), textMuted: ui.Hex("#9db4ae"),
			accent: ui.Hex("#7cc7b2"), selection: ui.RGBA(124, 199, 178, 0.30),
			codeBackground: ui.Hex("#263533"), quoteBorder: ui.Hex("#54706a"), rule: ui.Hex("#394946"),
		},
	},
	{
		id: "graphite",
		light: colorPalette{
			background: ui.Hex("#eff1f5"), header: ui.Hex("#e5e8ee"), status: ui.Hex("#e5e8ee"),
			surface: ui.Hex("#f9fafc"), border: ui.Hex("#d5dae3"),
			text: ui.Hex("#2b3440"), textMuted: ui.Hex("#636d7d"),
			accent: ui.Hex("#40639c"), selection: ui.RGBA(64, 99, 156, 0.26),
			codeBackground: ui.Hex("#e4e8ef"), quoteBorder: ui.Hex("#a9b3c2"), rule: ui.Hex("#d5dae3"),
		},
		dark: colorPalette{
			background: ui.Hex("#20242d"), header: ui.Hex("#272c37"), status: ui.Hex("#272c37"),
			surface: ui.Hex("#2b3140"), border: ui.Hex("#3e4553"),
			text: ui.Hex("#e5e9f0"), textMuted: ui.Hex("#a0aab8"),
			accent: ui.Hex("#84a9e8"), selection: ui.RGBA(132, 169, 232, 0.30),
			codeBackground: ui.Hex("#2b3140"), quoteBorder: ui.Hex("#5d6675"), rule: ui.Hex("#3e4553"),
		},
	},
	{
		// 手绘：浅色是米黄纸张 + 墨水字 + 朱红批注，选区像荧光笔；深色是夜间手账。
		id: "sketch",
		light: colorPalette{
			background: ui.Hex("#fbf6ea"), header: ui.Hex("#f3ead6"), status: ui.Hex("#f3ead6"),
			surface: ui.Hex("#fffcf3"), border: ui.Hex("#d8c9a8"),
			text: ui.Hex("#2d2a26"), textMuted: ui.Hex("#6b6252"),
			accent: ui.Hex("#b23a1e"), selection: ui.RGBA(255, 212, 64, 0.45),
			codeBackground: ui.Hex("#f2e9d4"), quoteBorder: ui.Hex("#e2b33a"), rule: ui.Hex("#cbb991"),
		},
		dark: colorPalette{
			background: ui.Hex("#262420"), header: ui.Hex("#2f2c26"), status: ui.Hex("#2f2c26"),
			surface: ui.Hex("#322e27"), border: ui.Hex("#4b4538"),
			text: ui.Hex("#efe7d6"), textMuted: ui.Hex("#b7ac95"),
			accent: ui.Hex("#f2a66e"), selection: ui.RGBA(240, 200, 90, 0.30),
			codeBackground: ui.Hex("#322e27"), quoteBorder: ui.Hex("#c9a23a"), rule: ui.Hex("#5a523f"),
		},
	},
	{
		// Claude：Anthropic 的暖象牙与近黑，橙 accent。浅深两套与代码配色
		// 取自 Mayoi（sorrycc/Mayoi）的同名主题。
		id: "claude",
		light: colorPalette{
			background: ui.Hex("#faf9f5"), header: ui.Hex("#f0eee6"), status: ui.Hex("#f0eee6"),
			surface: ui.Hex("#ffffff"), border: ui.Hex("#e3dfd3"),
			text: ui.Hex("#141413"), textMuted: ui.Hex("#6b675f"),
			accent: ui.Hex("#c96442"), selection: ui.RGBA(217, 119, 87, 0.22),
			codeBackground: ui.Hex("#f5f4ee"), quoteBorder: ui.Hex("#a39e93"), rule: ui.Hex("#e3dfd3"),
		},
		dark: colorPalette{
			background: ui.Hex("#1f1e1d"), header: ui.Hex("#191918"), status: ui.Hex("#191918"),
			surface: ui.Hex("#262523"), border: ui.Hex("#37342f"),
			text: ui.Hex("#e8e6dc"), textMuted: ui.Hex("#a39e93"),
			accent: ui.Hex("#d97757"), selection: ui.RGBA(217, 119, 87, 0.28),
			codeBackground: ui.Hex("#262523"), quoteBorder: ui.Hex("#6b675f"), rule: ui.Hex("#2e2c2a"),
		},
	},
	{
		// Qoder：Forest 的白绿浅色与灰绿深色，森林绿 accent。取自 Mayoi。
		id: "qoder",
		light: colorPalette{
			background: ui.Hex("#ffffff"), header: ui.Hex("#f2f4f2"), status: ui.Hex("#f2f4f2"),
			surface: ui.Hex("#ffffff"), border: ui.Hex("#e3e6e3"),
			text: ui.Hex("#141414"), textMuted: ui.Hex("#636261"),
			accent: ui.Hex("#4b6f5a"), selection: ui.RGBA(75, 111, 90, 0.22),
			codeBackground: ui.Hex("#f5f7f5"), quoteBorder: ui.Hex("#949391"), rule: ui.Hex("#e3e6e3"),
		},
		dark: colorPalette{
			background: ui.Hex("#191b1a"), header: ui.Hex("#242624"), status: ui.Hex("#242624"),
			surface: ui.Hex("#222523"), border: ui.Hex("#363c37"),
			text: ui.Hex("#d9dcd9"), textMuted: ui.Hex("#949d96"),
			accent: ui.Hex("#8ee5a1"), selection: ui.RGBA(77, 156, 97, 0.32),
			codeBackground: ui.Hex("#1c1f1d"), quoteBorder: ui.Hex("#646d66"), rule: ui.Hex("#363c37"),
		},
	},
}

// colorThemeByID 按 ID 找颜色主题；未知 ID 回退经典主题。
func colorThemeByID(id string) (colorThemeDef, bool) {
	for _, t := range colorThemes {
		if t.id == id {
			return t, true
		}
	}
	return colorThemeDef{}, false
}

// colorThemeNames 返回可选颜色主题的展示名；手绘只经手账风格生效，不在列表里。
func colorThemeNames() []string {
	names := make([]string, 0, len(colorThemes)+1)
	names = append(names, colorThemeDisplayName(colorThemeClassic))
	for _, t := range colorThemes {
		if t.id != styleSketch {
			names = append(names, colorThemeDisplayName(t.id))
		}
	}
	return names
}

// classicNeutralPalette 是经典颜色主题的中性灰变体：用于设置窗口等
// 没有点上下文的界面（主窗口仍随点配色）。
func classicNeutralPalette(dark bool) colorPalette {
	if dark {
		return colorPalette{
			background: ui.Hex("#1e1e20"), header: ui.Hex("#2a2a2c"), status: ui.Hex("#2a2a2c"),
			surface: ui.Hex("#2c2c2e"), border: ui.Hex("#3d3d40"),
			text: ui.Hex("#f2f2f4"), textMuted: ui.Hex("#a8a8ad"),
			accent: ui.Hex("#d1d1d6"), selection: ui.RGBA(209, 209, 214, 0.30),
			codeBackground: ui.Hex("#2c2c2e"), quoteBorder: ui.Hex("#5c5c61"), rule: ui.Hex("#3d3d40"),
		}
	}
	return colorPalette{
		background: ui.Hex("#ffffff"), header: ui.Hex("#f2f2f4"), status: ui.Hex("#f2f2f4"),
		surface: ui.Hex("#f2f2f4"), border: ui.Hex("#e0e0e3"),
		text: ui.Hex("#1d1d1f"), textMuted: ui.Hex("#6e6e73"),
		accent: ui.Hex("#3a3a3c"), selection: ui.RGBA(58, 58, 60, 0.22),
		codeBackground: ui.Hex("#f2f2f4"), quoteBorder: ui.Hex("#c7c7cc"), rule: ui.Hex("#e0e0e3"),
	}
}

// dotPalette 把第 i 个点的既有配色适配成语义调色板（经典颜色主题的主窗口）。
// 点配色沿用经典七点底色，保持原有体验。
func dotPalette(i int, dark bool) colorPalette {
	return colorPalette{
		background:     dotBackground(i, dark),
		header:         headerBackground(dark),
		status:         dotStatusBackground(i, dark),
		surface:        dotStatusBackground(i, dark).Mix(dotBackground(i, dark), 0.5),
		border:         dotStatusBackground(i, dark).Mix(dotBackground(i, dark), 0.72),
		text:           textOnBackground(i, dark),
		textMuted:      textOnBackground(i, dark).Mix(dotBackground(i, dark), 0.25),
		accent:         dotAccent(i, dark),
		selection:      selectionForDot(i, dark),
		codeBackground: dotStatusBackground(i, dark).Mix(dotBackground(i, dark), 0.35),
		quoteBorder:    dotAccent(i, dark).Mix(dotBackground(i, dark), 0.35),
		rule:           dotStatusBackground(i, dark).Mix(dotBackground(i, dark), 0.72),
	}
}

// textOnBackground 返回点背景上可读的正文色：点自带深浅底，正文取
// 系统主题的文字色在背景上做微调，保证 ≥ 4.5:1（浅底配深字，深底配浅字）。
func textOnBackground(i int, dark bool) ui.Color {
	bg := dotBackground(i, dark)
	if dark {
		return ui.Hex("#f4f2ef").Mix(bg, 0.06)
	}
	return ui.Hex("#26241f").Mix(bg, 0.04)
}

// selectionForDot 选区叠色：经典主题用点 accent 的低透明度版本，
// 叠加后文字仍可读（accent 亮度向背景方向收敛）。
func selectionForDot(i int, dark bool) ui.Color {
	acc, bg := colorFloats(dotAccent(i, dark)), colorFloats(dotBackground(i, dark))
	// accent 向背景靠拢 35%，避免高饱和叠色盖住文字。
	m := [3]float64{}
	for c := 0; c < 3; c++ {
		m[c] = acc[c] + (bg[c]-acc[c])*0.35
	}
	return ui.RGBA(uint8(m[0]*255+0.5), uint8(m[1]*255+0.5), uint8(m[2]*255+0.5), 0.38)
}

// colorFloats 返回颜色的 0–1 分量（对比度计算用）。
func colorFloats(c ui.Color) [4]float64 {
	return [4]float64{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, float64(c.A) / 255}
}

// ---- Markdown 排版主题 ----

// markdownLayout 是一套排版主题的静态参数（不携带颜色）。
type markdownLayout struct {
	id           string
	bodySize     float32 // 基准字号 px
	lineHeight   float32 // 基准行高
	blockGap     float32 // 块间距 px
	padX, padY   float32 // 正文内边距
	headingScale [6]float32
	// 结构尺寸
	codePadX, codePadY   float32 // 代码块内边距
	quoteIndent          float32 // 引用左缩进
	tablePadX, tablePadY float32 // 表格单元格内边距
	// 装饰尺寸：0 表示默认（引用竖条 3、代码圆角 8、代码无描边）。
	quoteBar   float32 // 引用竖条宽度
	codeRadius float32 // 代码块圆角
	codeBorder float32 // 代码块描边宽度
}

var markdownLayouts = []markdownLayout{
	{
		id:       "sonnet",
		bodySize: 16, lineHeight: 1.6, blockGap: 12,
		padX: 24, padY: 24,
		headingScale: [6]float32{1.75, 1.5, 1.25, 1.0625, 0.9375, 0.875},
		codePadX:     12, codePadY: 10, quoteIndent: 8, tablePadX: 8, tablePadY: 6,
	},
	{
		id:       "note",
		bodySize: 15, lineHeight: 1.5, blockGap: 12,
		padX: 24, padY: 24,
		headingScale: [6]float32{1.75, 1.5, 1.25, 1.0625, 0.9375, 0.875},
		codePadX:     12, codePadY: 10, quoteIndent: 8, tablePadX: 8, tablePadY: 6,
	},
	{
		id:       "clear",
		bodySize: 16, lineHeight: 1.65, blockGap: 14,
		padX: 26, padY: 26,
		headingScale: [6]float32{1.7, 1.45, 1.25, 1.08, 0.95, 0.88},
		codePadX:     12, codePadY: 10, quoteIndent: 8, tablePadX: 9, tablePadY: 7,
	},
	{
		id:       "book",
		bodySize: 17, lineHeight: 1.8, blockGap: 18,
		padX: 32, padY: 28,
		headingScale: [6]float32{1.5, 1.3, 1.15, 1.02, 0.95, 0.9},
		codePadX:     14, codePadY: 12, quoteIndent: 10, tablePadX: 10, tablePadY: 8,
	},
	{
		id:       "compact",
		bodySize: 15, lineHeight: 1.5, blockGap: 10,
		padX: 20, padY: 20,
		headingScale: [6]float32{1.6, 1.38, 1.2, 1.05, 0.93, 0.86},
		codePadX:     9, codePadY: 7, quoteIndent: 6, tablePadX: 6, tablePadY: 4,
	},
	{
		// 手账：疏朗行距、荧光笔式粗引用条、圆角描边代码卡，配合手绘颜色主题。
		id:       "sketch",
		bodySize: 16, lineHeight: 1.75, blockGap: 16,
		padX: 28, padY: 26,
		headingScale: [6]float32{1.6, 1.38, 1.2, 1.06, 0.96, 0.9},
		codePadX:     14, codePadY: 12, quoteIndent: 10, tablePadX: 9, tablePadY: 7,
		quoteBar: 5, codeRadius: 12, codeBorder: 1.5,
	},
}

// markdownLayoutDisplayName 排版主题的当前语言显示名；未知 ID 原样返回。
func markdownLayoutDisplayName(id string) string {
	switch id {
	case "sonnet":
		return locales.LayoutSonnet()
	case "note":
		return locales.LayoutNote()
	case "clear":
		return locales.LayoutClear()
	case "book":
		return locales.LayoutBook()
	case "compact":
		return locales.LayoutCompact()
	case "sketch":
		return locales.LayoutSketch()
	}
	return id
}

func markdownLayoutByID(id string) markdownLayout {
	for _, l := range markdownLayouts {
		if l.id == id {
			return l
		}
	}
	return markdownLayouts[0]
}

// markdownLayoutNames 返回全部排版主题的展示名。
func markdownLayoutNames() []string {
	names := make([]string, 0, len(markdownLayouts))
	for _, l := range markdownLayouts {
		names = append(names, markdownLayoutDisplayName(l.id))
	}
	return names
}

// ---- 字体栈（保留现有选项） ----

const (
	sonnetBodyFont = "JetBrains Mono, LXGW WenKai, 霞鹜文楷, LXGW WenKai Screen, Noto Serif SC, Georgia, serif"
	sonnetMonoFont = "JetBrains Mono, SF Mono, Menlo, Consolas, monospace"
	// 手写搭配：正文与标题同用霞鹜文楷 Lite（不随包分发；缺失时经字体运行时
	// 下载或本机注册，见 font_runtime.go）。栈里除注册名外只允许列 Apple 之外的
	// 名字作回退：未安装家族的字体匹配在无头环境（CI）会阻塞在系统按需字体下载上。
	// 不用 Noteworthy 等西文手写体：与中文字宽、笔画粗细不匹配，混排发乱。
	handBodyFont    = "LXGW WenKai Lite, LXGW WenKai, 霞鹜文楷, serif"
	handHeadingFont = handBodyFont
	// hannotateFont 全文手札体：中西文都用手札体，未安装时回退文楷。
	hannotateFont = handHeadingFont
)

// cjkFontStack 把中文字体键解析为字体栈。字体与排版主题解耦：
// ""（默认）、kai、sonnet（旧值别名）都是霞鹜文楷栈，与主题选择无关。
// 栈里保留自带的西文字形，供 latinFont 为空（跟随中文字体）时使用。
// Windows 回退（微软雅黑、宋体）排在 Apple 字体之后，macOS 渲染不受影响。
func cjkFontStack(key string) string {
	if family := fonts.LocalFamily(key); family != "" {
		return family + ", system-ui, sans-serif"
	}
	switch key {
	case "system", "pingfang":
		return "-apple-system, PingFang SC, Hiragino Sans GB, Microsoft YaHei UI, Microsoft YaHei, system-ui, sans-serif"
	case "songti":
		return "Songti SC, STSong, Georgia, Noto Serif SC, SimSun, serif"
	case "hand":
		return handBodyFont
	case "hannotate":
		return hannotateFont
	default:
		return sonnetBodyFont
	}
}

// latinFontStack 把西文字体键解析为字体栈；空串表示跟随中文字体。
// 这些字体都不含中文字形，因此放在字体栈最前面只会接管西文。
func latinFontStack(key string) string {
	if family := fonts.LocalFamily(key); family != "" {
		return family
	}
	switch key {
	case "fira":
		return "Fira Code"
	case "jetbrains":
		return "JetBrains Mono"
	case "system":
		return "-apple-system, SF Pro Text, Helvetica Neue, Segoe UI, Arial, sans-serif"
	case "noteworthy":
		return "Noteworthy"
	case "bradley":
		return "Bradley Hand"
	}
	return ""
}

// composeFontStack 组合西文与中文字体栈：西文在前，中文字形落到后面的中文栈。
// 西文为空时直接用中文栈，旧偏好文件的渲染结果因此保持不变。
func composeFontStack(latin, cjk string) string {
	if latin == "" {
		return cjk
	}
	return latin + ", " + cjk
}

// bodyFontStack 正文最终字体栈（中文字体键 + 西文字体键）。
func bodyFontStack(cjkKey, latinKey string) string {
	return composeFontStack(latinFontStack(latinKey), cjkFontStack(cjkKey))
}

// headingFontStack 标题最终字体栈：只有手写搭配的标题中文字体换成手札体，
// 其余跟随正文；西文部分始终与正文同一选择。
func headingFontStack(cjkKey, latinKey string) string {
	cjk := cjkFontStack(cjkKey)
	if cjkKey == "hand" {
		cjk = handHeadingFont
	}
	return composeFontStack(latinFontStack(latinKey), cjk)
}

// monoFontStack 是用户代码字体键到字体栈的解析，解耦规则同 bodyFontStack。
func monoFontStack(key string) string {
	if family := fonts.LocalFamily(key); family != "" {
		return family + ", monospace"
	}
	switch key {
	case "system":
		return "monospace"
	case "fira":
		return "Fira Code, monospace"
	case "sfmono":
		return "SF Mono, Menlo, monospace"
	case "menlo":
		return "Menlo, Monaco, monospace"
	default:
		return sonnetMonoFont
	}
}

// ---- 解析后的最终样式 ----

// aiColors 是建议卡与 diff 块的语义色（随颜色主题与明暗解析，派生 +
// 逐主题覆盖两层；见 aiColorsFor）。消费方是助理条建议卡（view_ai_bar.go）。
type aiColors struct {
	cardBg, cardBorder ui.Color // 建议卡面与描边
	delBg, delInk      ui.Color // 删除块底/字（红系）
	addBg, addInk      ui.Color // 新增块底/字（绿系）
}

// aiSeeds 是一套红/绿墨色锚点（浅/深各一份）。
type aiSeeds struct{ delL, addL, delD, addD ui.Color }

// aiSeedDefault 默认锚点：浅色用深墨红绿，深色用亮彩红绿。
var aiSeedDefault = aiSeeds{
	delL: ui.Hex("#9c3424"), addL: ui.Hex("#175c37"),
	delD: ui.Hex("#eb9a8a"), addD: ui.Hex("#8fd0a8"),
}

// aiSeedOverrides 随主题调色的锚点：手账用朱红批注与马克笔绿，claude/qoder
// 各随主题基调；未列出的主题用默认锚点。锚点只取墨色，底色在
// aiColorsFor 里以更低饱和度混出，避免红绿块在纸面上刺眼。
var aiSeedOverrides = map[string]aiSeeds{
	"sketch": {delL: ui.Hex("#8a4436"), addL: ui.Hex("#46583d"), delD: ui.Hex("#e8a188"), addD: ui.Hex("#a8d8b0")},
	"claude": {delL: ui.Hex("#8f4030"), addL: ui.Hex("#3d6b4f"), delD: ui.Hex("#e8a188"), addD: ui.Hex("#9ecdb0")},
	"qoder":  {delL: ui.Hex("#7e3a2c"), addL: ui.Hex("#2c5a3f"), delD: ui.Hex("#e3a08e"), addD: ui.Hex("#8ee5a1")},
}

// aiInkOn 让 seed 在 bg 上达到至少 4.6:1：不足时向该主题正文色逐步收敛
// （正文色已保证可读；红绿墨色与黑/白同向加深或提亮，收敛方向单调）。
func aiInkOn(seed, bg, text ui.Color) ui.Color {
	ink := seed
	for i := 0; i < 12 && contrastRatio(ink, bg) < 4.6; i++ {
		ink = ink.Mix(text, 0.18)
	}
	return ink
}

// aiColorsFor 解析一套调色板下的建议卡配色：红绿墨色自适应对比度，
// diff 底色 = 背景 + 少量墨色；卡片面复用控件表面，描边取强调与边界的混合。
// 经典主题（随点配色/中性灰变体）传 "classic"，走默认锚点但同样自适应。
func aiColorsFor(themeID string, pal colorPalette, dark bool) aiColors {
	seed := aiSeedDefault
	if s, ok := aiSeedOverrides[themeID]; ok {
		seed = s
	}
	delSeed, addSeed := seed.delL, seed.addL
	if dark {
		delSeed, addSeed = seed.delD, seed.addD
	}
	delInk := aiInkOn(delSeed, pal.background, pal.text)
	addInk := aiInkOn(addSeed, pal.background, pal.text)
	// diff 底色只掺 6-8% 墨色（柔和粉彩，两家模型审阅的一致结论：
	// 原先 10-12% 饱和度偏高）；卡面描边改用边界色而非强调色混合，
	// 让卡安静地浮在纸面上。
	delBg := pal.background.Mix(delInk, 0.06)
	addBg := pal.background.Mix(addInk, 0.08)
	// 底色掺了墨色后对比略降，再校一轮。
	delInk = aiInkOn(delInk, delBg, pal.text)
	addInk = aiInkOn(addInk, addBg, pal.text)
	return aiColors{
		cardBg: pal.surface, cardBorder: pal.border.Mix(pal.text, 0.2),
		delBg: delBg, delInk: delInk,
		addBg: addBg, addInk: addInk,
	}
}

// resolvedStyle 是主窗口/设置窗口实际使用的样式：颜色 + 排版已合并。
type resolvedStyle struct {
	// 颜色
	background, header, status  ui.Color
	surface, border             ui.Color
	text, textMuted             ui.Color
	accent, selection           ui.Color
	codeBackground, quoteBorder ui.Color
	rule                        ui.Color
	// AI 建议卡配色（派生 + 主题覆盖，见 aiColorsFor）。
	ai aiColors
	// 排版
	bodyFont, monoFont      string
	headingFont             string // 标题字体：默认同正文，手写搭配为手札体
	bodySize, lineHeight    float32
	blockGap                float32
	padX, padY              float32
	headingSizes            [6]float32
	codeSize, codeLineHeigh float32
	codePadX, codePadY      float32
	quoteIndent             float32
	tablePadX, tablePadY    float32
	quoteBar, codeRadius    float32
	codeBorder              float32
	sketch                  bool // 手账排版：引用/代码/分隔线/标题改为手绘装饰
}

// colorThemeClassic 是经典颜色主题（随点配色）的 ID，对应更名前旧文件里的
// "tot"；未知 ID 一律回退到它。
const colorThemeClassic = "classic"

// colorPaletteFor 解析颜色层：经典主题在 dot 上下文（主窗口）随点配色，
// 其余界面用中性灰变体；其他颜色主题用固定调色板。
func colorPaletteFor(themeID string, dot int, dark bool) colorPalette {
	if themeID == colorThemeClassic || themeID == "" {
		if dot >= 0 {
			return dotPalette(dot, dark)
		}
		return classicNeutralPalette(dark)
	}
	if def, ok := colorThemeByID(themeID); ok {
		if dark {
			return def.dark
		}
		return def.light
	}
	return classicNeutralPalette(dark)
}

// styleCacheKey 是 resolveStyle 的全部输入：外观偏好、调用参数，以及本机字体
// 列表的身份（usableFontKey 依赖它；枚举完成时整片替换，不原地改）。
type styleCacheKey struct {
	appearance     appearancePrefs
	dot            int
	dark, override bool
	families       *ui.FontFamily
	familyCount    int
}

type styleCacheEntry struct {
	key   styleCacheKey
	style resolvedStyle
}

// styleCache 是解析结果的小缓存：一帧里外框与每个可见块都要样式，
// 选了本机字体时每次解析还要扫描系统字体列表。条目很少（明暗 × 场景）。
type styleCache struct {
	entries [4]styleCacheEntry
	used    int
	next    int
}

// resolveStyle 合并三层：颜色主题 + Markdown 排版 + 用户排版覆盖。
// override 为 false 时忽略用户字体/字号/行高/内边距覆盖（预览卡片用）。
func (a *app) resolveStyle(dot int, dark bool, override bool) resolvedStyle {
	key := styleCacheKey{appearance: a.prefs.Appearance, dot: dot, dark: dark, override: override,
		familyCount: len(a.fonts.families)}
	if len(a.fonts.families) > 0 {
		key.families = &a.fonts.families[0]
	}
	cache := &a.fonts.styles
	for i := range cache.used {
		if cache.entries[i].key == key {
			return cache.entries[i].style
		}
	}
	s := a.computeStyle(dot, dark, override)
	cache.entries[cache.next] = styleCacheEntry{key: key, style: s}
	cache.next = (cache.next + 1) % len(cache.entries)
	cache.used = min(cache.used+1, len(cache.entries))
	return s
}

func (a *app) computeStyle(dot int, dark bool, override bool) resolvedStyle {
	pal := colorPaletteFor(a.prefs.Appearance.EffectiveColorTheme(), dot, dark)
	layout := markdownLayoutByID(a.prefs.Appearance.EffectiveMarkdownTheme())
	s := resolvedStyle{
		background: pal.background, header: pal.header, status: pal.status,
		surface: pal.surface, border: pal.border, text: pal.text, textMuted: pal.textMuted,
		accent: pal.accent, selection: pal.selection,
		codeBackground: pal.codeBackground, quoteBorder: pal.quoteBorder, rule: pal.rule,
		ai: aiColorsFor(a.prefs.Appearance.EffectiveColorTheme(), pal, dark),

		// 字体与排版主题解耦：始终用用户选择的字体键（空 = 默认栈）。
		bodyFont:    bodyFontStack(a.usableFontKey(a.prefs.Appearance.BodyFont), a.usableFontKey(a.prefs.Appearance.LatinFont)),
		headingFont: headingFontStack(a.usableFontKey(a.prefs.Appearance.BodyFont), a.usableFontKey(a.prefs.Appearance.LatinFont)),
		monoFont:    monoFontStack(a.usableFontKey(a.prefs.Appearance.MonoFont)),
		bodySize:    layout.bodySize,
		lineHeight:  layout.lineHeight,
		blockGap:    layout.blockGap,
		padX:        layout.padX, padY: layout.padY,
		codePadX: layout.codePadX, codePadY: layout.codePadY,
		quoteIndent:   layout.quoteIndent,
		tablePadX:     layout.tablePadX,
		tablePadY:     layout.tablePadY,
		codeSize:      roundHalf(layout.bodySize * 0.875),
		codeLineHeigh: roundHalf(layout.lineHeight*0.98 + 0.02),
		quoteBar:      orFloat(layout.quoteBar, 3),
		codeRadius:    orFloat(layout.codeRadius, 8),
		codeBorder:    layout.codeBorder,
		sketch:        layout.id == "sketch",
	}
	for i, scale := range layout.headingScale {
		s.headingSizes[i] = roundHalf(layout.bodySize * scale)
	}
	if override {
		// 用户显式覆盖优先；主题切换不重置覆盖值。字体已解耦，无需覆盖分支。
		if a.prefs.Appearance.BodySize > 0 {
			s.bodySize = a.prefs.Appearance.BodySize
			for i := range s.headingSizes {
				s.headingSizes[i] = roundHalf(s.bodySize * layout.headingScale[i])
			}
			s.codeSize = roundHalf(s.bodySize * 0.875)
		}
		if a.prefs.Appearance.LineHeight > 0 {
			s.lineHeight = a.prefs.Appearance.LineHeight
			s.codeLineHeigh = roundHalf(s.lineHeight*0.98 + 0.02)
		}
		if a.prefs.Appearance.PadX > 0 {
			s.padX = a.prefs.Appearance.PadX
		}
		if a.prefs.Appearance.PadY > 0 {
			s.padY = a.prefs.Appearance.PadY
		}
	}
	return s
}

// apply 把解析后的样式写进 ui.Theme（内容区主题）。
func (s resolvedStyle) apply(t ui.Theme) ui.Theme {
	t.Background, t.Text, t.TextMuted = s.background, s.text, s.textMuted
	t.Surface, t.Border, t.Accent = s.surface, s.border, s.accent
	t.Selection, t.Font, t.FontSize = s.selection, s.bodyFont, s.bodySize
	return t
}

func orFloat(v, def float32) float32 {
	if v > 0 {
		return v
	}
	return def
}

func roundHalf(v float32) float32 {
	return float32(math.Round(float64(v)*2) / 2)
}

// ---- 对比度辅助（测试与预览共用） ----

// relLum 相对亮度（WCAG 定义）。
func relLum(c ui.Color) float64 {
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	cf := colorFloats(c)
	return 0.2126*lin(cf[0]) + 0.7152*lin(cf[1]) + 0.0722*lin(cf[2])
}

// contrastRatio 两色的 WCAG 对比度。
func contrastRatio(a, b ui.Color) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// dotTheme 是经典七点配色的每点主题（早期版本采样自同类型便签应用的
// 成品资源，数值保持不变）。
type dotTheme struct {
	dot  string // 点本身的颜色
	accL string // 浅色模式 accent（高亮/链接）
	accD string // 深色模式 accent
	bgL  string // 浅色模式内容区背景
	bgD  string // 深色模式内容区背景
	stL  string // 浅色模式顶栏/底栏背景（status_background）
	stD  string // 深色模式顶栏/底栏背景
}

var dotThemes = [DotCount]dotTheme{
	{"#d4253a", "#883341", "#ffbdc0", "#feeeef", "#211414", "#f5d5d5", "#3d282b"}, // 1 红
	{"#f67500", "#7e3f13", "#fac198", "#fff2e9", "#241c12", "#f2dac9", "#453222"}, // 2 橙
	{"#ffd01c", "#776200", "#f4dea3", "#fff9ea", "#211f0b", "#f4e9ce", "#3e3c1f"}, // 3 黄
	{"#65c802", "#58752e", "#d3efac", "#f9fff0", "#1b2015", "#dee8cb", "#313724"}, // 4 绿
	{"#0f75e9", "#3857a2", "#94bfff", "#e7eeff", "#191d26", "#cdd8f8", "#2c354e"}, // 5 蓝
	{"#a40bd8", "#754993", "#dfb5fc", "#f8effe", "#1e1723", "#e3d3f1", "#372943"}, // 6 紫
	{"#98989d", "#3a3a3c", "#d1d1d6", "#ffffff", "#0a0a0c", "#f2f2f4", "#0e0e10"}, // 7 灰
}

// dotColor 返回第 i 个点的颜色。
func dotColor(i int) ui.Color { return ui.Hex(dotThemes[i].dot) }

// dotAccent 返回第 i 个点的 accent 色（随深浅色模式）。
func dotAccent(i int, dark bool) ui.Color {
	if dark {
		return ui.Hex(dotThemes[i].accD)
	}
	return ui.Hex(dotThemes[i].accL)
}

// dotBackground 返回第 i 个点的内容区背景色（随深浅色模式）。
func dotBackground(i int, dark bool) ui.Color {
	if dark {
		return ui.Hex(dotThemes[i].bgD)
	}
	return ui.Hex(dotThemes[i].bgL)
}

// dotStatusBackground 返回第 i 个点的底栏背景色。
func dotStatusBackground(i int, dark bool) ui.Color {
	if dark {
		return ui.Hex(dotThemes[i].stD)
	}
	return ui.Hex(dotThemes[i].stL)
}

// headerBackground 顶栏固定色：经典主题的顶栏不随点变，浅色 #F2F2F2。
func headerBackground(dark bool) ui.Color {
	if dark {
		return ui.Hex("#2a2a2c")
	}
	return ui.Hex("#f2f2f2")
}

// themeIsDark 用背景亮度判断当前是否深色主题。
func themeIsDark(t *ui.Theme) bool {
	c := t.Background
	lum := (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
	return lum < 0.5
}
