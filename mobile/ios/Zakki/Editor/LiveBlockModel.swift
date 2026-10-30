import Foundation
import Zakkicore

// MARK: - 实时预览块模型（M2-6，同构桌面 live_blocks.go / live_projection.go）

// 桌面实时预览的 Typora 式投影在移动端的移植：raw 按桌面 parseLines 规则切块，
// 每块再投影出「可见文字」（块级/行内标记剥离），并给出可见↔raw 双向映射与
// 编辑写回（replacing/fragment）。全文件偏移一律为 UTF-16 code unit（与
// NSString/NSRange、FindTimeToken 绑定、现有 EditorDocument 管线同一坐标系）；
// 桌面源码是 rune 坐标，标记字符均为 ASCII，语义逐条对齐，坐标宽度按 UTF-16 解释。
// 本文件纯 Foundation + core 绑定，不依赖 UIKit。
// 按职责分文件：分块解析 LiveBlockParsing.swift、行级语法原语 LiveSyntax.swift、
// 跨块结构编辑 LiveDocEditing.swift、可见文字投影 LiveProjection.swift；
// 本文件保留块类型与块列表坐标（模型基座）。

// MARK: 块类型与块

enum LiveBlockKind: Equatable {
    case para
    case heading
    case quote
    case code
    case rule
    case list
    case task
    case blank
}

// 行内标记 span：raw 内 UTF-16 区间，含标记符本身（对齐桌面 mSpan）。
enum LiveSpanKind: Equatable {
    case bold
    case italic
    case code
    case link
}

struct LiveSpan: Equatable {
    var start: Int
    var end: Int
    var kind: LiveSpanKind
}

// 从文档切出的一个可编辑块（对齐桌面 liveBlock）。
struct LiveBlock: Equatable {
    var kind: LiveBlockKind
    var startLine: Int // 文档起始行（含）
    var endLine: Int // 文档结束行（含）
    var raw: String // 原始 markdown（行间以 \n 相连）
    var level: Int = 0 // 标题级别（1-4，桌面 parseLines 同款钳制）
    var number: Int = 0 // 有序列表序号（0 = 无序）
    var checked: Bool = false // 待办勾选
    var codeLines: [String] = [] // 代码块内容行（不含围栏行）
    var fenceInfo: String = "" // 代码块开栏 info 串
    var timeTokenRaw: String? // 正文首个时间令牌原文（识别只走 core 绑定）

    // 视觉身份：集合视图 diff 只重载真正变化的块（行号平移不算变化）。
    var visualIdentity: String {
        "\(kind)|\(level)|\(number)|\(checked)|\(raw)|\(fenceInfo)"
    }
}

// MARK: 块列表与坐标

// 分块结果：blocks + 每行在全文中的 UTF-16 起点（行间以 \n 相连，末尾无换行）。
struct LiveBlockList {
    let blocks: [LiveBlock]
    let lineStarts: [Int]
    let raw: String

    var count: Int { blocks.count }

    // 块 i 的全文 UTF-16 起点。
    func startOffset(of i: Int) -> Int {
        guard blocks.indices.contains(i) else { return (raw as NSString).length }
        return lineStarts[blocks[i].startLine]
    }

    // 全文偏移 → 所在块索引与块起点（对齐桌面 blockOfRuneOffset：
    // 文档 = 各块 raw 依次相连，块间由换行分隔，末尾无换行）。
    func blockOf(_ offset: Int) -> (index: Int, blockStart: Int) {
        for (i, block) in blocks.enumerated() {
            let start = startOffset(of: i)
            if offset < start + LiveSyntax.length(block.raw) + 1 {
                return (i, start)
            }
        }
        if blocks.isEmpty { return (0, 0) }
        let last = blocks.count - 1
        return (last, startOffset(of: last))
    }
}
