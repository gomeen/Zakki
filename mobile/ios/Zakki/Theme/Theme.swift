import UIKit

// 移动端最小主题集（M2-3）。数值从桌面源码提取（internal/app/view_theme.go、
// tab_model.go、tab_palette.go），按规格主题数据不进 core、不经绑定分发，
// 漂移由 ThemeSyncTests 读桌面 Go 源码核对。
// M2 只取桌面默认组合：经典颜色主题的无点中性变体 + sonnet 排版，无切换 UI。

// sRGB 三元组：主题数值的最小表示。混色保持与桌面 ui.Color.Mix 同一语义
//（uint8 通道、float32 中间值、+0.5 截断），移植结果逐位一致（TabPillTests 验证）。
struct ThemeRGB: Equatable {
    var r: UInt8
    var g: UInt8
    var b: UInt8

    init(_ r: UInt8, _ g: UInt8, _ b: UInt8) {
        self.r = r
        self.g = g
        self.b = b
    }

    var uiColor: UIColor {
        UIColor(red: CGFloat(r) / 255, green: CGFloat(g) / 255, blue: CGFloat(b) / 255, alpha: 1)
    }

    // ui.Color.Mix 的 sRGB 路径：t 是从 self 向 other 的位移比例。
    func mix(_ other: ThemeRGB, _ t: Float) -> ThemeRGB {
        let t = max(0, min(t, 1))
        func channel(_ a: UInt8, _ b: UInt8) -> UInt8 {
            UInt8(Float(a) * (1 - t) + Float(b) * t + 0.5)
        }
        return ThemeRGB(channel(r, other.r), channel(g, other.g), channel(b, other.b))
    }
}

// WCAG 相对亮度与对比度（对齐桌面 view_theme.go 的 relLum/contrastRatio，
// Double 精度；胶囊收敛的二分分支依赖这两个函数的逐位一致）。
func themeRelLuminance(_ c: ThemeRGB) -> Double {
    func linear(_ v: Double) -> Double {
        v <= 0.03928 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4)
    }
    return 0.2126 * linear(Double(c.r) / 255)
        + 0.7152 * linear(Double(c.g) / 255)
        + 0.0722 * linear(Double(c.b) / 255)
}

func themeContrastRatio(_ a: ThemeRGB, _ b: ThemeRGB) -> Double {
    let la = themeRelLuminance(a), lb = themeRelLuminance(b)
    let (hi, lo) = la >= lb ? (la, lb) : (lb, la)
    return (hi + 0.05) / (lo + 0.05)
}

// 选区叠色：桌面 colorPalette.selection 是带透明度的 RGBA。
struct ThemeSelectionColor: Equatable {
    var rgb: ThemeRGB
    var alpha: Float

    var uiColor: UIColor {
        UIColor(
            red: CGFloat(rgb.r) / 255, green: CGFloat(rgb.g) / 255,
            blue: CGFloat(rgb.b) / 255, alpha: CGFloat(alpha)
        )
    }
}

// 一套颜色主题在某个明暗模式下的语义色（字段集对齐桌面 colorPalette）。
struct ThemeSemanticPalette: Equatable {
    var background: ThemeRGB
    var header: ThemeRGB
    var status: ThemeRGB
    var surface: ThemeRGB
    var border: ThemeRGB
    var text: ThemeRGB
    var textMuted: ThemeRGB
    var accent: ThemeRGB
    var selection: ThemeSelectionColor
    var codeBackground: ThemeRGB
    var quoteBorder: ThemeRGB
    var rule: ThemeRGB
}

enum Theme {
    // MARK: 语义色

    // 桌面经典颜色主题的无点中性变体（view_theme.go classicNeutralPalette）：
    // 桌面把它用于没有点上下文的界面；移动端 M2 无随点配色窗口，全套取这一套。
    static let classicNeutralLight = ThemeSemanticPalette(
        background: ThemeRGB(0xFF, 0xFF, 0xFF), header: ThemeRGB(0xF2, 0xF2, 0xF4), status: ThemeRGB(0xF2, 0xF2, 0xF4),
        surface: ThemeRGB(0xF2, 0xF2, 0xF4), border: ThemeRGB(0xE0, 0xE0, 0xE3),
        text: ThemeRGB(0x1D, 0x1D, 0x1F), textMuted: ThemeRGB(0x6E, 0x6E, 0x73),
        accent: ThemeRGB(0x3A, 0x3A, 0x3C), selection: ThemeSelectionColor(rgb: ThemeRGB(0x3A, 0x3A, 0x3C), alpha: 0.22),
        codeBackground: ThemeRGB(0xF2, 0xF2, 0xF4), quoteBorder: ThemeRGB(0xC7, 0xC7, 0xCC), rule: ThemeRGB(0xE0, 0xE0, 0xE3)
    )

    static let classicNeutralDark = ThemeSemanticPalette(
        background: ThemeRGB(0x1E, 0x1E, 0x20), header: ThemeRGB(0x2A, 0x2A, 0x2C), status: ThemeRGB(0x2A, 0x2A, 0x2C),
        surface: ThemeRGB(0x2C, 0x2C, 0x2E), border: ThemeRGB(0x3D, 0x3D, 0x40),
        text: ThemeRGB(0xF2, 0xF2, 0xF4), textMuted: ThemeRGB(0xA8, 0xA8, 0xAD),
        accent: ThemeRGB(0xD1, 0xD1, 0xD6), selection: ThemeSelectionColor(rgb: ThemeRGB(0xD1, 0xD1, 0xD6), alpha: 0.30),
        codeBackground: ThemeRGB(0x2C, 0x2C, 0x2E), quoteBorder: ThemeRGB(0x5C, 0x5C, 0x61), rule: ThemeRGB(0x3D, 0x3D, 0x40)
    )

    // 明暗动态色：跟随系统（iOS 无桌面「点上下文」概念，浅深各取对应语义色）。
    enum Semantic {
        static let background = dynamic(\.background)
        static let header = dynamic(\.header)
        static let status = dynamic(\.status)
        static let surface = dynamic(\.surface)
        static let border = dynamic(\.border)
        static let text = dynamic(\.text)
        static let textMuted = dynamic(\.textMuted)
        static let accent = dynamic(\.accent)
        static let selection = dynamicSelection(\.selection)
        static let codeBackground = dynamic(\.codeBackground)
        static let quoteBorder = dynamic(\.quoteBorder)
        static let rule = dynamic(\.rule)

        private static func dynamic(_ keyPath: KeyPath<ThemeSemanticPalette, ThemeRGB>) -> UIColor {
            UIColor { traits in
                let pal = traits.userInterfaceStyle == .dark ? Theme.classicNeutralDark : Theme.classicNeutralLight
                return pal[keyPath: keyPath].uiColor
            }
        }

        private static func dynamicSelection(_ keyPath: KeyPath<ThemeSemanticPalette, ThemeSelectionColor>) -> UIColor {
            UIColor { traits in
                let pal = traits.userInterfaceStyle == .dark ? Theme.classicNeutralDark : Theme.classicNeutralLight
                return pal[keyPath: keyPath].uiColor
            }
        }
    }

    // MARK: 排版（桌面默认排版主题 sonnet）

    enum Typography {
        static let bodySize: CGFloat = 16
        static let lineHeight: CGFloat = 1.6
        static let blockGap: CGFloat = 12
        static let padX: CGFloat = 24
        static let padY: CGFloat = 24
        // 标题比例（h1–h6 相对正文的倍数）与换算后的绝对字号。
        static let headingScale: [CGFloat] = [1.75, 1.5, 1.25, 1.0625, 0.9375, 0.875]
        static let headingSizes: [CGFloat] = headingScale.map { roundHalf(bodySize * $0) }
        static let codeSize: CGFloat = roundHalf(bodySize * 0.875)
        // 桌面代码行高：lineHeight*0.98 + 0.02 后取半值。
        static let codeLineHeight: CGFloat = roundHalf(lineHeight * 0.98 + 0.02)
        static let codePadX: CGFloat = 12
        static let codePadY: CGFloat = 10
        static let quoteIndent: CGFloat = 8
        static let tablePadX: CGFloat = 8
        static let tablePadY: CGFloat = 6
        // 桌面排版未给值时的缺省装饰尺寸。
        static let quoteBar: CGFloat = 3
        static let codeRadius: CGFloat = 8

        // 组件（时间胶囊）几何：spike 组件规格的几何部分，颜色走主题推导。
        static let pillSize: CGFloat = 12
        static let pillHeight: CGFloat = 22
        static let pillRadius: CGFloat = 11
        static let pillPadH: CGFloat = 8
        static let pillBorder: CGFloat = 1

        // 字体等价方案（待定项保守解）：桌面 LXGW WenKai + JetBrains Mono 下载
        // 字体体系不在 M2 分发范围，用系统 sans/等宽，排版数值仍对齐桌面。
        static let bodyFont = UIFont.systemFont(ofSize: bodySize)
        static let codeFont = UIFont.monospacedSystemFont(ofSize: codeSize, weight: .regular)

        static func headingFont(level: Int) -> UIFont {
            let index = min(max(level, 0), headingSizes.count - 1)
            return .systemFont(ofSize: headingSizes[index], weight: .bold)
        }

        // 正文行高取 CSS line-height 语义（字号 × 倍数）；UIFont.lineHeight 是
        // 字体自身行盒，差额用 lineSpacing 补足。
        static let bodyParagraph: NSParagraphStyle = {
            let style = NSMutableParagraphStyle()
            style.lineSpacing = bodySize * lineHeight - bodyFont.lineHeight
            return style
        }()

        // 桌面 roundHalf：数值收敛到 0.5 步进。
        private static func roundHalf(_ v: CGFloat) -> CGFloat {
            (v * 2).rounded() / 2
        }
    }

    // MARK: 时间胶囊色

    struct PillColors: Equatable {
        var background: ThemeRGB
        var text: ThemeRGB
        var borderColor: ThemeRGB?
    }

    // 浅深两套都从所选语义色推导（桌面无独立 pill 主题色；spike 的朱红底/
    // 语义反演占位值不作依据）。常规 = 控件表面 + 次级文字 + 边框；重复 =
    // accent 向表面渗底（比例对齐胶囊底色收敛上限）+ accent 文字按底线收敛。
    static func pillColors(recurring: Bool, dark: Bool) -> PillColors {
        let pal = dark ? classicNeutralDark : classicNeutralLight
        if !recurring {
            return PillColors(background: pal.surface, text: pal.textMuted, borderColor: pal.border)
        }
        let background = pal.surface.mix(pal.accent, TabPill.maxMix)
        let toward: ThemeRGB = dark ? ThemeRGB(255, 255, 255) : ThemeRGB(0, 0, 0)
        let text = TabPill.contrastMix(
            base: pal.accent, toward: toward, against: background,
            target: TabPill.contrastMin, limit: 1
        )
        return PillColors(background: background, text: text, borderColor: nil)
    }
}

// 标签色板（tab_model.go tabPaletteColors：前 7 色为 Tot 点色）与胶囊显示色
// 收敛（tab_palette.go tabPillColors/tabContrastMix 的逐位移植）。
enum ThemeTagPalette {
    static let colors: [String] = [
        "#d4253a", "#f67500", "#ffd01c", "#65c802", "#0f75e9", "#a40bd8",
        "#98989d", "#00a3b4", "#f7429c", "#a2845e", "#5856d6", "#32ade6",
    ]

    // 槽位回退（桌面 tabColor：i % len；负数防御与 Swift 语义一致）。
    static func hex(at index: Int) -> String {
        let n = colors.count
        return colors[((index % n) + n) % n]
    }

    // 槽位回退色（非可选：内置调色板恒可解析）。
    static func color(at index: Int) -> ThemeRGB {
        parse(hex(at: index))!
    }

    // #rgb/#rrggbb（桌面 validTabColor 口径）；非法返回 nil。
    static func parse(_ hex: String) -> ThemeRGB? {
        var body = hex.trimmingCharacters(in: .whitespacesAndNewlines)
        if body.hasPrefix("#") {
            body.removeFirst()
        }
        guard body.count == 3 || body.count == 6 else { return nil }
        guard let value = UInt64(body, radix: 16) else { return nil }
        if body.count == 3 {
            let r = UInt8((value >> 8) & 0xF), g = UInt8((value >> 4) & 0xF), b = UInt8(value & 0xF)
            return ThemeRGB(r * 17, g * 17, b * 17)
        }
        return ThemeRGB(UInt8((value >> 16) & 0xFF), UInt8((value >> 8) & 0xFF), UInt8(value & 0xFF))
    }
}

// 标签胶囊显示色：不透明胶囊的底色向黑白收敛（≤12%）争取 5:1、保底 4.5:1；
// 文字取对比更大的黑白软色，不足时向纯黑白收敛。只派生显示色，不改标签原色。
enum TabPill {
    struct Palette: Equatable {
        var background: ThemeRGB
        var text: ThemeRGB
    }

    static let contrastTarget = 5.0
    static let contrastMin = 4.5
    static let maxMix: Float = 0.12

    private static let softInkLight = ThemeRGB(248, 248, 250)
    private static let softInkDark = ThemeRGB(36, 36, 38)

    static func palette(base: ThemeRGB) -> Palette {
        var text = softInkLight
        var toward = ThemeRGB(0, 0, 0)
        if themeContrastRatio(softInkDark, base) > themeContrastRatio(softInkLight, base) {
            text = softInkDark
            toward = ThemeRGB(255, 255, 255)
        }
        let background = contrastMix(base: base, toward: toward, against: text, target: contrastTarget, limit: maxMix)
        if themeContrastRatio(text, background) >= contrastMin {
            return Palette(background: background, text: text)
        }
        // 任意不透明底色至少有一种纯黑白字达到 4.5:1；只在柔和字色不足时兜底。
        let white = ThemeRGB(255, 255, 255), black = ThemeRGB(0, 0, 0)
        let extreme = themeContrastRatio(black, background) > themeContrastRatio(white, background) ? black : white
        let fixedText = contrastMix(base: text, toward: extreme, against: background, target: contrastMin, limit: 1)
        return Palette(background: background, text: fixedText)
    }

    // 有界混色内寻找最小改动（tabContrastMix）：预算不足保留边界色；
    // 12 轮二分的 float32 步进与桌面一致，保证收敛结果逐位相同。
    static func contrastMix(base: ThemeRGB, toward: ThemeRGB, against: ThemeRGB, target: Double, limit: Float) -> ThemeRGB {
        if themeContrastRatio(base, against) >= target {
            return base
        }
        var result = base.mix(toward, limit)
        if themeContrastRatio(result, against) < target {
            return result
        }
        var lo: Float = 0
        var hi = limit
        for _ in 0..<12 {
            let mid = (lo + hi) / 2
            let candidate = base.mix(toward, mid)
            if themeContrastRatio(candidate, against) >= target {
                hi = mid
                result = candidate
            } else {
                lo = mid
            }
        }
        return result
    }
}
