import XCTest
@testable import Zakki

// 胶囊对比度收敛的移植正确性：golden 值由桌面真实实现导出（go test 打印
// tabPillColors 对 12 色调色板的结果），混色与二分须逐位一致。
final class TabPillTests: XCTestCase {
    // 桌面 tabPillColors(ui.Hex(base)) 的输出（internal/app 导出，2026 待 M2-3）。
    static let golden: [(base: String, background: String, text: String)] = [
        ("#d4253a", "#ce2438", "#f8f8fa"),
        ("#f67500", "#f67500", "#242426"),
        ("#ffd01c", "#ffd01c", "#242426"),
        ("#65c802", "#65c802", "#242426"),
        ("#0f75e9", "#0d69d1", "#f8f8fa"),
        ("#a40bd8", "#a40bd8", "#f8f8fa"),
        ("#98989d", "#98989d", "#242426"),
        ("#00a3b4", "#00a3b4", "#242426"),
        ("#f7429c", "#f854a5", "#242426"),
        ("#a2845e", "#aa8e6b", "#242426"),
        ("#5856d6", "#5856d6", "#f8f8fa"),
        ("#32ade6", "#32ade6", "#242426"),
    ]

    private func rgb(_ hex: String) throws -> ThemeRGB {
        guard let value = ThemeTagPalette.parse(hex) else {
            XCTFail("测试用 hex 非法: \(hex)")
            return ThemeRGB(255, 0, 255)
        }
        return value
    }

    // MARK: golden

    func testPaletteMatchesDesktopGolden() throws {
        for (base, background, text) in Self.golden {
            let got = TabPill.palette(base: try rgb(base))
            XCTAssertEqual(got.background, try rgb(background), "\(base) 背景与桌面不一致")
            XCTAssertEqual(got.text, try rgb(text), "\(base) 文字与桌面不一致")
        }
    }

    // MARK: 收敛规则不变式（桌面 tab_palette_test 口径）

    // 稳定态始终 ≥ 4.5:1；背景向黑白混合不超过 12%。
    func testAllPaletteColorsMeetContrastFloor() throws {
        for hex in ThemeTagPalette.colors {
            let pill = TabPill.palette(base: try rgb(hex))
            XCTAssertGreaterThanOrEqual(
                themeContrastRatio(pill.text, pill.background), TabPill.contrastMin,
                "\(hex) 胶囊文字对比度低于底线"
            )
        }
    }

    // 对比已达标时原色原样保留（浅黄/橙配深字不混色）。
    func testAlreadyContrastingBaseUnchanged() throws {
        let orange = try rgb("#f67500")
        let pill = TabPill.palette(base: orange)
        XCTAssertEqual(pill.background, orange)
    }

    // 混色的 uint8 通道语义（+0.5 截断）与桌面 ui.Color.Mix 一致。
    func testMixChannelRounding() {
        let red = ThemeRGB(212, 37, 58)
        XCTAssertEqual(red.mix(ThemeRGB(0, 0, 0), 0.12), ThemeRGB(187, 33, 51))
        XCTAssertEqual(red.mix(ThemeRGB(255, 255, 255), 0), red)
        XCTAssertEqual(red.mix(ThemeRGB(0, 0, 0), 1), ThemeRGB(0, 0, 0))
    }

    // 有界混色的预算边界：预算内达不到目标时保留边界色（桌面同款口径）。
    func testContrastMixKeepsBoundaryWhenBudgetExhausted() {
        let base = ThemeRGB(128, 128, 128)
        let ink = ThemeRGB(36, 36, 38)
        let got = TabPill.contrastMix(base: base, toward: ThemeRGB(0, 0, 0), against: ink, target: 5, limit: 0.01)
        XCTAssertEqual(got, base.mix(ThemeRGB(0, 0, 0), 0.01))
    }

    // MARK: 明暗动态色

    // 同一令牌在两种外观下解析为对应语义色（明暗跟随系统的运行时行为）。
    func testSemanticColorsResolvePerAppearance() {
        let light = UITraitCollection(userInterfaceStyle: .light)
        let dark = UITraitCollection(userInterfaceStyle: .dark)
        XCTAssertEqual(
            Theme.Semantic.background.resolvedColor(with: light),
            Theme.classicNeutralLight.background.uiColor
        )
        XCTAssertEqual(
            Theme.Semantic.background.resolvedColor(with: dark),
            Theme.classicNeutralDark.background.uiColor
        )
        XCTAssertEqual(
            Theme.Semantic.selection.resolvedColor(with: dark),
            Theme.classicNeutralDark.selection.uiColor
        )
    }

    // 胶囊位图按 traits 渲染：两套外观都出图且尺寸一致。
    func testTimePillRendersPerAppearance() {
        let info = TimeTokenInfo(due: "2026-01-01 10:00")
        let light = TimePill.image(for: info, traits: UITraitCollection(userInterfaceStyle: .light))
        let dark = TimePill.image(for: info, traits: UITraitCollection(userInterfaceStyle: .dark))
        XCTAssertEqual(light.size, dark.size)
        XCTAssertGreaterThan(light.size.width, 0)
    }

    // MARK: 时间胶囊色（主题推导）

    // 常规项 = 控件表面 + 次级文字 + 边框；重复项 = accent 渗底 + accent 收敛文字。
    func testTimePillColorsDeriveFromSemanticPalette() { // 无解析依赖，不需 throws
        for dark in [false, true] {
            let pal = dark ? Theme.classicNeutralDark : Theme.classicNeutralLight

            let regular = Theme.pillColors(recurring: false, dark: dark)
            XCTAssertEqual(regular.background, pal.surface)
            XCTAssertEqual(regular.text, pal.textMuted)
            XCTAssertEqual(regular.borderColor, pal.border)

            let recurring = Theme.pillColors(recurring: true, dark: dark)
            XCTAssertEqual(recurring.background, pal.surface.mix(pal.accent, TabPill.maxMix))
            XCTAssertGreaterThanOrEqual(
                themeContrastRatio(recurring.text, recurring.background), TabPill.contrastMin,
                "dark=\(dark) 重复胶囊文字对比度低于底线"
            )
            XCTAssertNil(recurring.borderColor)
        }
    }
}
