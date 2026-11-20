package app

// 内嵌的 Lucide 图标（ISC 许可），统一 24×24 viewBox、2px 圆头描边。
// mygo 的 ui.Icon 会把它们画成文字色，尺寸用 Size 统一。

import "github.com/egoist/mygo/ui"

func lucide(inner string) *ui.SVG {
	return ui.MustParseSVG([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` +
			inner + `</svg>`))
}

// iconSolid 实心填充图标：与线性版同一 24×24 网格轮廓，选中态切换用。
func iconSolid(inner string) *ui.SVG {
	return ui.MustParseSVG([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor">` +
			inner + `</svg>`))
}

var (
	iconTable       = lucide(`<rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/>`)
	iconAlignLeft   = lucide(`<path d="M4 6h16M4 12h10M4 18h16"/>`)
	iconAlignCenter = lucide(`<path d="M4 6h16M7 12h10M4 18h16"/>`)
	iconAlignRight  = lucide(`<path d="M4 6h16M10 12h10M4 18h16"/>`)
	iconTrash       = lucide(`<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>`)
	iconCloseWindow = lucide(`<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/>`)
	// iconPin 窗口置顶（Lucide pin）
	iconPin = lucide(`<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/>`)
	// iconPinFilled / iconEyeFilled 与线性版同一 Lucide 轮廓的实心版本：
	// 图钉 = 原闭合轮廓填充 + 实心针杆；眼睛 = 原杏仁形挖出 iris 圆孔。
	iconPinFilled = iconSolid(`<path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/><path d="M10.75 16.5h2.5V21a1.25 1.25 0 0 1-2.5 0z"/>`)
	iconEyeFilled = iconSolid(`<path fill-rule="evenodd" d="M2.06 12.35a1 1 0 0 1 0-.7 10.75 10.75 0 0 1 19.88 0 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-19.88 0zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z"/>`)
	// iconPreview markdown 预览（Lucide eye）
	iconPreview = lucide(`<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>`)
	// 查找条：上一个 / 下一个 / 关闭（Lucide chevron-up、chevron-down、x）
	iconChevronUp   = lucide(`<path d="m18 15-6-6-6 6"/>`)
	iconChevronDown = lucide(`<path d="m6 9 6 6 6-6"/>`)
	iconX           = lucide(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	// 快捷键页：恢复默认与系统保留（Lucide rotate-ccw、lock）
	iconRotateCCW = lucide(`<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/>`)
	iconLock      = lucide(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`)
	// iconSearch 点内搜索（Lucide search）
	iconSearch = lucide(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	// iconGear 设置侧栏：通用（Lucide settings）
	iconGear = lucide(`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`)
	// iconDatabase 设置侧栏：备份与数据（Lucide database）
	iconDatabase = lucide(`<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/>`)
	// iconKeyboard 设置侧栏：快捷键（Lucide keyboard）
	iconKeyboard = lucide(`<path d="M10 8h.01"/><path d="M12 12h.01"/><path d="M14 8h.01"/><path d="M16 12h.01"/><path d="M18 8h.01"/><path d="M6 8h.01"/><path d="M7 16h10"/><path d="M8 12h.01"/><rect width="20" height="16" x="2" y="4" rx="2"/>`)
	// iconLayers 设置侧栏：标签与外观（Lucide layers）
	iconLayers = lucide(`<path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>`)
	// iconSparkles 设置侧栏与助理条：AI 助理（Lucide sparkles）
	iconSparkles = lucide(`<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/><path d="M4 17v2"/><path d="M5 18H3"/>`)
	// iconGlobe 向导语言步骤（Lucide globe）
	iconGlobe = lucide(`<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>`)
	// iconFolder 向导备份文件夹（Lucide folder）
	iconFolder = lucide(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
)
