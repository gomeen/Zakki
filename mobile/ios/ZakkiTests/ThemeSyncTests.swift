import XCTest
@testable import Zakki

// 防漂移同步测试：读桌面 Go 源码（#filePath 推回仓库根），解析主题常量，
// 核对 Swift 主题文件与桌面一致。故意改坏任一侧数值都会红。
// 只解析主题常量字面量，不涉及任何时间语义。
final class ThemeSyncTests: XCTestCase {
    // mobile/ios/ZakkiTests/ → 仓库根。
    private static let repoRoot = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()

    private func goSource(_ relative: String) throws -> String {
        let url = Self.repoRoot.appendingPathComponent(relative)
        return try String(contentsOf: url, encoding: .utf8)
    }

    // 正则全部捕获组；无匹配返回空数组（断言失败时给出可读信息）。
    private func matches(_ pattern: String, in source: String) throws -> [[String]] {
        let regex = try NSRegularExpression(pattern: pattern)
        let range = NSRange(source.startIndex..., in: source)
        return regex.matches(in: source, range: range).map { result in
            (1..<result.numberOfRanges).map { index in
                result.range(at: index).location == NSNotFound
                    ? "" : (source as NSString).substring(with: result.range(at: index))
            }
        }
    }

    private func assertEqual(_ got: ThemeRGB, _ hex: String, _ label: String) throws {
        XCTAssertEqual(got, try XCTUnwrap(ThemeTagPalette.parse(hex), label), label)
    }

    // MARK: 桌面默认主题的选择依据

    // M2 取「桌面默认」组合的依据必须仍在：Default() 的颜色/排版主题 ID。
    func testDesktopDefaultsAreClassicAndSonnet() throws {
        let prefs = try goSource("internal/prefs/prefs.go")
        XCTAssertEqual(try matches(#"ColorTheme:\s+"classic","#, in: prefs).count, 1, "桌面默认颜色主题不再是 classic")
        XCTAssertEqual(try matches(#"MarkdownTheme:\s+"sonnet","#, in: prefs).count, 1, "桌面默认排版主题不再是 sonnet")
    }

    // MARK: 语义色（classicNeutralPalette）

    func testSemanticPaletteMatchesDesktop() throws {
        let source = try goSource("internal/app/view_theme.go")
        let start = try XCTUnwrap(source.range(of: "func classicNeutralPalette"), "找不到 classicNeutralPalette")
        let end = try XCTUnwrap(source.range(of: "// dotPalette 把第 i 个点"), "找不到 classicNeutralPalette 结束边界")
        let body = String(source[start.upperBound..<end.lowerBound])

        // 源码顺序：dark 块在前、light 在后；Hex 字段各 11 个，RGBA 为 selection。
        let hexes = try matches(##"(\w+):\s*ui\.Hex\("#([0-9a-fA-F]{6})"\)"##, in: body)
        guard hexes.count == 22 else {
            XCTFail("Hex 字段数应为 22（dark 11 + light 11），实际 \(hexes.count)")
            return
        }
        let rgbas = try matches(#"(\w+):\s*ui\.RGBA\((\d+),\s*(\d+),\s*(\d+),\s*([0-9.]+)\)"#, in: body)
        guard rgbas.count == 2 else {
            XCTFail("RGBA 字段应为 selection 的 dark/light 各一个，实际 \(rgbas.count)")
            return
        }

        let hexOrder = ["background", "header", "status", "surface", "border", "text", "textMuted", "accent", "codeBackground", "quoteBorder", "rule"]
        for (offset, pal) in [(11, Theme.classicNeutralDark), (22, Theme.classicNeutralLight)] {
            let slice = Array(hexes[offset - 11..<offset])
            for (index, field) in slice.enumerated() {
                XCTAssertEqual(field[0], hexOrder[index], "字段顺序变化，需同步调整 Theme 与本测试")
                try assertEqual(fieldForPalette(pal, hexOrder[index]), field[1], "\(field[0]) 与桌面不一致")
            }
            let sel = rgbas[offset == 11 ? 0 : 1]
            XCTAssertEqual(sel[0], "selection")
            XCTAssertEqual(
                pal.selection,
                ThemeSelectionColor(
                    rgb: ThemeRGB(UInt8(sel[1])!, UInt8(sel[2])!, UInt8(sel[3])!),
                    alpha: Float(sel[4])!
                ),
                "selection 与桌面不一致"
            )
        }
    }

    private func fieldForPalette(_ pal: ThemeSemanticPalette, _ field: String) -> ThemeRGB {
        switch field {
        case "background": return pal.background
        case "header": return pal.header
        case "status": return pal.status
        case "surface": return pal.surface
        case "border": return pal.border
        case "text": return pal.text
        case "textMuted": return pal.textMuted
        case "accent": return pal.accent
        case "codeBackground": return pal.codeBackground
        case "quoteBorder": return pal.quoteBorder
        case "rule": return pal.rule
        default: fatalError("未知语义色字段 \(field)")
        }
    }

    // MARK: 排版（sonnet）

    func testTypographyMatchesDesktopSonnet() throws {
        let source = try goSource("internal/app/view_theme.go")
        let pattern = #"id:\s*"sonnet",\s*bodySize:\s*([0-9.]+),\s*lineHeight:\s*([0-9.]+),\s*blockGap:\s*([0-9.]+),\s*padX:\s*([0-9.]+),\s*padY:\s*([0-9.]+),\s*headingScale:\s*\[6\]float32\{([0-9.,\s]+)\},\s*codePadX:\s*([0-9.]+),\s*codePadY:\s*([0-9.]+),\s*quoteIndent:\s*([0-9.]+),\s*tablePadX:\s*([0-9.]+),\s*tablePadY:\s*([0-9.]+),"#
        let rows = try matches(pattern, in: source)
        guard rows.count == 1, rows[0].count == 11 else {
            XCTFail("sonnet 排版块解析失败（应恰一处 11 个捕获组），实际 \(rows.count) 行")
            return
        }
        let row = rows[0]

        let bodySize = CGFloat(Double(row[0])!)
        let lineHeight = CGFloat(Double(row[1])!)
        XCTAssertEqual(Theme.Typography.bodySize, bodySize, "bodySize 与桌面不一致")
        XCTAssertEqual(Theme.Typography.lineHeight, lineHeight, "lineHeight 与桌面不一致")
        XCTAssertEqual(Theme.Typography.blockGap, CGFloat(Double(row[2])!), "blockGap 与桌面不一致")
        XCTAssertEqual(Theme.Typography.padX, CGFloat(Double(row[3])!), "padX 与桌面不一致")
        XCTAssertEqual(Theme.Typography.padY, CGFloat(Double(row[4])!), "padY 与桌面不一致")

        // 桌面 roundHalf 推导：标题绝对字号、代码字号与行高。
        let scales = row[5].split(separator: ",").map { CGFloat(Double($0.trimmingCharacters(in: .whitespaces))!) }
        XCTAssertEqual(scales.count, 6)
        XCTAssertEqual(Theme.Typography.headingScale, scales, "headingScale 与桌面不一致")
        XCTAssertEqual(
            Theme.Typography.headingSizes,
            scales.map { roundHalf(bodySize * $0) },
            "标题字号推导与桌面 roundHalf 不一致"
        )
        XCTAssertEqual(Theme.Typography.codeSize, roundHalf(bodySize * 0.875), "codeSize 推导与桌面不一致")
        XCTAssertEqual(Theme.Typography.codeLineHeight, roundHalf(lineHeight * 0.98 + 0.02), "codeLineHeight 推导与桌面不一致")

        XCTAssertEqual(Theme.Typography.codePadX, CGFloat(Double(row[6])!), "codePadX 与桌面不一致")
        XCTAssertEqual(Theme.Typography.codePadY, CGFloat(Double(row[7])!), "codePadY 与桌面不一致")
        XCTAssertEqual(Theme.Typography.quoteIndent, CGFloat(Double(row[8])!), "quoteIndent 与桌面不一致")
        XCTAssertEqual(Theme.Typography.tablePadX, CGFloat(Double(row[9])!), "tablePadX 与桌面不一致")
        XCTAssertEqual(Theme.Typography.tablePadY, CGFloat(Double(row[10])!), "tablePadY 与桌面不一致")

        // 装饰尺寸缺省值（computeStyle 的 orFloat 兜底）。
        XCTAssertEqual(try matches(#"orFloat\(layout\.quoteBar,\s*([0-9.]+)\)"#, in: source).first?.first, "3", "引用竖条缺省宽变化")
        XCTAssertEqual(try matches(#"orFloat\(layout\.codeRadius,\s*([0-9.]+)\)"#, in: source).first?.first, "8", "代码圆角缺省值变化")
        XCTAssertEqual(Theme.Typography.quoteBar, 3)
        XCTAssertEqual(Theme.Typography.codeRadius, 8)
    }


    // MARK: 标签色板与胶囊收敛常数

    func testTagPaletteMatchesDesktop() throws {
        let source = try goSource("internal/app/tab_model.go")
        let start = try XCTUnwrap(source.range(of: "var tabPaletteColors = [12]string{"), "找不到 tabPaletteColors")
        let end = try XCTUnwrap(source.range(of: "}", range: start.upperBound..<source.endIndex), "找不到 tabPaletteColors 结束边界")
        let hexes = try matches(##""#([0-9a-fA-F]{6})""##, in: String(source[start.upperBound..<end.lowerBound]))
        XCTAssertEqual(hexes.count, 12, "调色板应为 12 色")
        XCTAssertEqual(
            ThemeTagPalette.colors,
            hexes.map { "#" + $0[0].lowercased() },
            "标签调色板与桌面不一致"
        )
    }

    func testTabPillConstantsMatchDesktop() throws {
        let source = try goSource("internal/app/tab_palette.go")
        XCTAssertEqual(try matches(#"tabPillContrastTarget\s*=\s*([0-9.]+)"#, in: source).first?.first, "5.0")
        XCTAssertEqual(try matches(#"tabPillContrastMin\s*=\s*([0-9.]+)"#, in: source).first?.first, "4.5")
        XCTAssertEqual(try matches(#"tabPillMaxMix\s*=\s*float32\(([0-9.]+)\)"#, in: source).first?.first, "0.12")
        XCTAssertEqual(TabPill.contrastTarget, 5.0)
        XCTAssertEqual(TabPill.contrastMin, 4.5)
        XCTAssertEqual(TabPill.maxMix, 0.12)
    }

    // MARK: 工具（与桌面 roundHalf 同式，供推导核对）

    private func roundHalf(_ v: CGFloat) -> CGFloat {
        (v * 2).rounded() / 2
    }
}
