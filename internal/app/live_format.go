package app

// 格式命令执行：菜单、Keymap 与编辑器共用命令注册表（menu_commands.go），
// 这里负责在当前文档上下文中执行 Markdown 命令。
//
//   - 命令作用于当前主文档编辑选区或光标所在块；源码与实时预览都支持。
//   - 每次命令是一个独立撤销事务（editCommand），恢复文本与选区。
//   - 无选区时行内命令切换「后续输入格式」意图，不提前写空标记。
//   - 设置窗口/录键控件聚焦时不执行主文档命令。

import (
	"log/slog"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// runFormatCommand 命令入口：只在主窗口拥有焦点（或无窗口焦点）时执行，
// 避免设置窗口里输入时误触发主文档格式命令。
func (a *app) runFormatCommand(id string) {
	if !a.winOpen {
		return
	}
	if focused := mygo.FocusedWindow(); focused != nil && focused != a.win {
		return // 焦点在设置窗口：不执行主文档命令
	}
	a.withWindow(func() { a.execFormatCommand(id) })
}

func (a *app) execFormatCommand(id string) {
	switch id {
	case cmdBold:
		a.execInline(spanBold)
	case cmdItalic:
		a.execInline(spanItalic)
	case cmdInlineCode:
		a.execInline(spanCode)
	case cmdLink:
		a.openLinkEditor()
	case cmdH1, cmdH2, cmdH3, cmdH4, cmdH5, cmdH6:
		a.execHeading(int(id[len(id)-1]) - int('0'))
	case cmdBody:
		a.execHeading(0)
	case cmdQuote:
		a.execPrefix(prefixQuote)
	case cmdBullet:
		a.execPrefix(prefixBullet)
	case cmdOrdered:
		a.execPrefix(prefixOrdered)
	case cmdTask:
		a.execPrefix(prefixTask)
	case cmdTaskTime:
		a.openTimePopoverAtCaret()
	case cmdCodeBlock:
		a.execCodeBlock()
	case cmdRule:
		a.execInsert("---")
	case cmdTable:
		a.execInsert(defaultTableBlock())
	case cmdAssistant:
		a.openAIBar()
	default:
		slog.Warn("未知格式命令", "id", id)
	}
}

// ---- 选区上下文 ----

// editorCaret 当前编辑光标与选区（文档 rune 偏移）。
func (a *app) editorCaret() (start, end int) {
	if a.selDoc {
		return max(a.selDocStart, 0), max(a.selDocEnd, 0)
	}
	if a.cfg.Source {
		start, end = a.srcCaret, a.srcSelEnd
		if a.srcSelSyncStart >= 0 {
			start, end = a.srcSelSyncStart, a.srcSelSyncEnd
		}
		return max(start, 0), max(end, 0)
	}
	start, end = a.mdCaret, a.mdSelEnd
	if a.selSyncStart >= 0 {
		start, end = a.selSyncStart, a.selSyncEnd
	}
	if a.mdActive < 0 {
		return 0, 0
	}
	// mdCaret 是块内 rune 位；换算成文档绝对偏移。
	pos := 0
	for i, b := range a.mdBlocks() {
		if i == a.mdActive {
			break
		}
		pos += utf8.RuneCountInString(b.raw) + 1
	}
	return pos + max(start, 0), pos + max(end, 0)
}

// ensureFocusBlock 实时预览态先建立焦点块（命令需要块上下文）。
func (a *app) ensureFocusBlock() {
	if a.cfg.Source || a.mdActive >= 0 {
		return
	}
	a.focusBlockEnd(len(a.mdBlocks()) - 1)
}

// applyDocResult 把变换结果写回当前点并恢复焦点/选区（实时预览与源码共用）。
func (a *app) applyDocResult(res docResult) {
	if !res.changed {
		return
	}
	wasDocSelection := a.selDoc
	a.activeTab().Text = res.doc
	if a.cfg.Source {
		a.srcSelSyncStart, a.srcSelSyncEnd = res.selStart, res.selEnd
		a.onEdited()
		return
	}
	a.clearDocSelection()
	blocks := a.mdBlocks()
	idx, off := a.blockOfRuneOffset(res.selStart)
	a.mdActive = idx
	a.mdDraft = blocks[idx].raw
	a.selSyncStart = res.selStart - off
	a.selSyncEnd = res.selEnd - off
	blockEnd := off + utf8.RuneCountInString(blocks[idx].raw)
	if res.selEnd > blockEnd || wasDocSelection && res.selStart != res.selEnd {
		a.selDoc, a.selDocStart, a.selDocEnd = true, res.selStart, res.selEnd
		a.selSyncEnd = a.selSyncStart
	}
	a.liveFocus, a.scrollActive = 1, true
	a.onEdited()
}

// runAsCommand 以独立撤销事务执行一次文档变换。
func (a *app) runAsCommand(fn func() docResult) {
	before := a.liveSnapshot()
	a.pendingEditKind = editCommand
	res := fn()
	a.applyDocResult(res)
	a.rememberLiveEdit(before, editCommand)
	if res.changed {
		a.syncMenus()
	}
}

// ---- 行内格式 ----

func (a *app) execInline(kind spanKind) {
	a.ensureFocusBlock()
	start, end := a.editorCaret()
	if start == end {
		if a.cfg.Source {
			return // 源码模式无待输入状态：无选区时不动作
		}
		a.togglePendingFormat(kind)
		a.syncMenus()
		return
	}
	// 普通实时预览选区限制在焦点块内；文档级选区覆盖全文行。
	from, to := 0, docLineCount(a.activeTab().Text)-1
	if !a.cfg.Source && !a.selDoc {
		blocks := a.mdBlocks()
		if a.mdActive < 0 || a.mdActive >= len(blocks) {
			return
		}
		blk := blocks[a.mdActive]
		if blk.kind == bkCode {
			return // 代码块内禁用行内格式
		}
		from, to = blk.start, blk.end
	}
	a.runAsCommand(func() docResult {
		return applyInlineToLines(a.activeTab().Text, from, to, start, end, kind)
	})
}

// togglePendingFormat 无选区时切换「后续输入格式」意图：仅改变待输入状态，
// 不提前写空标记。光标移动、换块或换点后重新按当前位置计算。
// 同一时刻只保留一个待输入格式，避免多重嵌套标记的歧义。
func (a *app) togglePendingFormat(kind spanKind) {
	state := a.openStateAtCaret()
	cur := state[kind]
	if v, ok := a.mdPending[kind]; ok {
		cur = v // 已有意图时再次切换在意图上取反
	}
	a.mdPending = map[spanKind]bool{}
	a.mdPending[kind] = !cur
	a.lastPendingCaret = a.mdCaret
	a.pendingBlock = a.mdActive
}

// maybeExpirePending 光标移动/换块后清零待输入意图（重新按当前位置计算）。
func (a *app) maybeExpirePending() {
	if len(a.mdPending) > 0 && (a.mdCaret != a.lastPendingCaret || a.mdActive != a.pendingBlock) {
		a.mdPending = nil
	}
}

// openStateAtCaret 计算光标当前位置上已经“打开”的行内格式
// （处于未闭合标记内部，例如刚输入 ** 之后）。
func (a *app) openStateAtCaret() map[spanKind]bool {
	state := map[spanKind]bool{}
	if a.cfg.Source || a.mdActive < 0 {
		return state
	}
	blocks := a.mdBlocks()
	if a.mdActive >= len(blocks) {
		return state
	}
	blk := blocks[a.mdActive]
	content, _ := blockContentPrefix(blk, a.mdCaret)
	return openInlineState(content)
}

// blockContentPrefix 返回块内容中光标之前的源文本（去块前缀）。
func blockContentPrefix(blk liveBlock, caret int) (string, int) {
	p := projectBlock(blk)
	if caret < p.start {
		caret = p.start
	}
	if caret > p.end {
		caret = p.end
	}
	rs := []rune(blk.raw)
	return string(rs[p.start:caret]), p.start
}

// openInlineState 扫描前缀里未闭合的行内标记。
func openInlineState(content string) map[spanKind]bool {
	state := map[spanKind]bool{}
	rs := []rune(content)
	spans := inlineSpans(content)
	consumed := make([]bool, len(rs))
	for _, sp := range spans {
		for i := sp.start; i < sp.end && i < len(consumed); i++ {
			consumed[i] = true
		}
	}
	for i := 0; i < len(rs); i++ {
		if consumed[i] {
			continue
		}
		switch rs[i] {
		case '`':
			state[spanCode] = !state[spanCode]
		case '*':
			if i+1 < len(rs) && rs[i+1] == '*' && !consumed[i+1] {
				state[spanBold] = !state[spanBold]
				i++
			} else {
				state[spanItalic] = !state[spanItalic]
			}
		}
	}
	return state
}

// replaceProjectedInput 把一次可见文本编辑映射回 Markdown，并消费待输入格式。
// 原生输入、菜单粘贴和右键粘贴必须共用这里，避免格式意图残留到下一次输入。
func (a *app) replaceProjectedInput(p liveProjection, blk liveBlock, next string, caret, oldStart, oldEnd int) (raw string, sourceCaret int, wrapped bool) {
	raw, sourceCaret = p.replaceSelection(next, caret, oldStart, oldEnd)
	pre, post, cutStart, cutEnd := a.pendingWrapFor(p, oldStart, oldEnd, blk, sourceCaret)
	if pre == "" && post == "" {
		return raw, sourceCaret, false
	}
	raw, sourceCaret = applyMarkerWrap(raw, p.sourceOffset(oldStart), sourceCaret, pre, post, cutStart, cutEnd)
	a.mdPending, a.lastPendingCaret, a.pendingBlock = nil, 0, -1
	return raw, sourceCaret, true
}

// pendingWrapFor 计算「待输入格式」在本次输入上要生成的标记。
// 返回插在输入前后的标记串，以及需一并删除的冗余闭合标记区间
// （新 raw 坐标，恒在输入区之后，见 completedStyleAt）；
// 不需要包裹时标记串为空。
func (a *app) pendingWrapFor(p liveProjection, oldStart, oldEnd int, blk liveBlock, caret int) (pre, post string, cutStart, cutEnd int) {
	if oldStart != oldEnd || len(a.mdPending) == 0 || caret <= p.sourceOffset(oldStart) {
		return "", "", 0, 0
	}
	if blk.kind == bkCode {
		return "", "", 0, 0 // 代码块内无行内格式
	}
	srcAt := p.sourceOffset(oldStart)
	content, _ := blockContentPrefix(blk, clamp(srcAt, 0, utf8.RuneCountInString(blk.raw)))
	state := openInlineState(content)
	for _, kind := range []spanKind{spanBold, spanItalic, spanCode} {
		intent, ok := a.mdPending[kind]
		if !ok {
			continue
		}
		open, closer := inlineMarkers(kind)
		switch {
		case intent && !state[kind]:
			pre += open
			post = closer + post
		case !intent && state[kind]:
			pre += closer // 关闭已打开的格式，输入落在格式之外
			if s, ok := completedStyleAt(p, kind, oldStart, srcAt); ok {
				// 输入已被插进完整 span 内部：新增的闭合标记使原闭合
				// 标记冗余，不同步删除会残留悬空 opener。
				shift := caret - srcAt
				cutStart, cutEnd = s.close+shift, s.tail+shift
			}
		}
	}
	// 相邻同标记会产生歧义 run：放弃包裹（丢弃意图，不破坏文档）。
	rs := []rune(p.raw)
	at := srcAt
	if pre != "" && at > 0 {
		if last := []rune(pre); rs[at-1] == last[len(last)-1] {
			return "", "", 0, 0
		}
	}
	if post != "" && at < len(rs) {
		if rs[at] == []rune(post)[0] {
			return "", "", 0, 0
		}
	}
	return pre, post, cutStart, cutEnd
}

// completedStyleAt 找光标可见位置所在的完整行内 span，且输入落点在其闭合
// 标记之前（块末尾 span 的视觉末尾、或 span 内容中间）。这种情况的「已打开」
// 来自前缀截断（闭合标记在 srcAt 之后），关闭时要删掉原闭合标记。
func completedStyleAt(p liveProjection, kind spanKind, visible, srcAt int) (projectedStyle, bool) {
	for _, s := range p.styles {
		if s.kind == kind && s.start <= visible && visible <= s.end && srcAt <= s.close {
			return s, true
		}
	}
	return projectedStyle{}, false
}

// applyMarkerWrap 把前后标记插到 raw 的 [at, caret) 输入区间两侧；
// cutStart/cutEnd 非零时删除该冗余区间（位于 caret 之后，
// 如被新增闭合标记取代的原闭合标记）。
func applyMarkerWrap(raw string, at, caret int, pre, post string, cutStart, cutEnd int) (string, int) {
	at = clamp(at, 0, utf8.RuneCountInString(raw))
	caret = clamp(caret, at, utf8.RuneCountInString(raw))
	bAt, bCaret := runeToByte(raw, at), runeToByte(raw, caret)
	out := raw[:bAt] + pre + raw[bAt:bCaret] + post
	if cutEnd > cutStart && cutStart >= caret {
		bStart, bEnd := runeToByte(raw, cutStart), runeToByte(raw, cutEnd)
		out += raw[bCaret:bStart] + raw[bEnd:]
	} else {
		out += raw[bCaret:]
	}
	return out, caret + utf8.RuneCountInString(pre) + utf8.RuneCountInString(post)
}

// ---- 标题 / 列表 / 引用 ----

// execHeading level 0=正文，1-6=标题。代码块/表格内不适用（菜单禁用理由见 Tooltip）。
func (a *app) execHeading(level int) {
	a.ensureFocusBlock()
	if !a.cfg.Source && !a.selDoc && a.blockKindAtActive().disallowBlockFormat() {
		return
	}
	from, to, caret := a.commandLineRange()
	a.runAsCommand(func() docResult {
		return applyHeadingToLines(a.activeTab().Text, from, to, caret, level)
	})
}

func (a *app) execPrefix(kind prefixKind) {
	a.ensureFocusBlock()
	if !a.cfg.Source && !a.selDoc && a.blockKindAtActive().disallowBlockFormat() {
		return
	}
	from, to, caret := a.commandLineRange()
	a.runAsCommand(func() docResult {
		return applyPrefixToLines(a.activeTab().Text, from, to, caret, kind)
	})
}

// blockKindAtActive 返回实时预览焦点块的类别（源码模式返回 bkPara）。
func (a *app) blockKindAtActive() blockKind {
	if a.cfg.Source || a.mdActive < 0 {
		return bkPara
	}
	blocks := a.mdBlocks()
	if a.mdActive >= len(blocks) {
		return bkPara
	}
	return blocks[a.mdActive].kind
}

// disallowBlockFormat 报告该块上下文是否禁用标题/列表/引用命令。
func (k blockKind) disallowBlockFormat() bool { return k == bkCode || k == bkTable }

// commandLineRange 命令作用的行范围：源码模式取选区覆盖的行；
// 实时预览取焦点块（行内变换限制在块内，跨块语义由块模型保证）。
func (a *app) commandLineRange() (from, to, caret int) {
	doc := a.activeTab().Text
	start, end := a.editorCaret()
	from, _ = caretLineCol(doc, start)
	to, _ = caretLineCol(doc, max(end, start))
	if !a.cfg.Source && !a.selDoc {
		blocks := a.mdBlocks()
		if a.mdActive >= 0 && a.mdActive < len(blocks) {
			blk := blocks[a.mdActive]
			if blk.kind != bkTable && blk.kind != bkCode {
				// 段落/标题/列表等行级块：当前行。
				caretLineInBlock, _ := caretLineCol(blk.raw, clamp(a.mdCaret, 0, utf8.RuneCountInString(blk.raw)))
				from, to = blk.start+caretLineInBlock, blk.start+caretLineInBlock
			} else {
				from, to = blk.start, blk.end
			}
		}
	}
	caret = start
	return from, to, caret
}

// ---- 代码块 ----

func (a *app) execCodeBlock() {
	a.ensureFocusBlock()
	from, to, caret := a.commandLineRange()
	if !a.cfg.Source && !a.selDoc {
		blocks := a.mdBlocks()
		if a.mdActive >= 0 && a.mdActive < len(blocks) {
			blk := blocks[a.mdActive]
			from, to = blk.start, blk.end
			if blk.kind == bkBlank {
				// 空块上执行：原地变空代码块。
				a.runAsCommand(func() docResult {
					return replaceLineWith(a.activeTab().Text, blk.start, "```\n\n```", 4)
				})
				return
			}
		}
	}
	a.runAsCommand(func() docResult {
		return applyCodeBlockToggle(a.activeTab().Text, from, to, caret)
	})
}

// ---- 插入 ----

func (a *app) execInsert(block string) {
	a.ensureFocusBlock()
	_, to, _ := a.commandLineRange()
	afterLine := to
	if !a.cfg.Source {
		blocks := a.mdBlocks()
		if a.mdActive >= 0 && a.mdActive < len(blocks) {
			afterLine = blocks[a.mdActive].end
		}
	}
	caretInBlock := len(block)
	if block == "---" {
		caretInBlock = 3
	} else if block == defaultTableBlock() {
		caretInBlock = 2 // 表头第一格内
	}
	a.runAsCommand(func() docResult {
		return applyInsertAfterLine(a.activeTab().Text, afterLine, block, caretInBlock)
	})
}

// ---- 源码模式按键（撤销/粘贴事务归口） ----

// recordSourceKey 源码编辑器输入拦截：撤销/重做走文档级事务，
// 粘贴记为独立事务。
func (a *app) recordSourceKey(ev ui.InputEvent) bool {
	if ev.Kind == ui.InputCommand && (ev.Text == "undo" || ev.Text == "redo") {
		a.liveUndoPending = 1
		if ev.Text == "redo" {
			a.liveUndoPending = -1
		}
		return true
	}
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyZ &&
		(ev.Mods == ui.Super || ev.Mods == ui.Ctrl || ev.Mods == ui.Super|ui.Shift || ev.Mods == ui.Ctrl|ui.Shift) {
		a.liveUndoPending = 1
		if ev.Mods&ui.Shift != 0 {
			a.liveUndoPending = -1
		}
		return true
	}
	if ev.Kind == ui.InputCommand && ev.Text == "paste" {
		a.nextEditIsPaste = true
	}
	return false
}

// takePendingEditKind 取走本帧待记录的事务类型。
func (a *app) takePendingEditKind() editKind {
	kind := a.pendingEditKind
	a.pendingEditKind = editInput
	return kind
}

// consumePendingEditKind 是 takePendingEditKind 的别名（源码模式用）。
func (a *app) consumePendingEditKind() editKind { return a.takePendingEditKind() }
