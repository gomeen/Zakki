package app

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

// ---- 实时预览（Typora 式） ----

// livePane 把文档按块渲染：焦点块保留排版结构并只编辑可见文字，其余块即时排版；
// 点击渲染块切换焦点块（live_edit.go 负责状态与光标映射）。
func (a *app) livePane(c *ui.Context, t *ui.Theme, dark bool) {
	a.ensureDraftSynced()
	doc := a.activeTab().Text
	blocks := a.mdBlocks()

	// 搜索跳转：把命中所在块切为焦点块，选区停在命中处。
	if a.jumpPending {
		a.jumpPending = false
		if ms := a.docMatches(doc, a.query); len(ms) > 0 {
			a.matchIdx = clamp(a.matchIdx, 0, len(ms)-1)
			m := ms[a.matchIdx]
			idx, off := a.blockOfRuneOffset(m.Start)
			if idx != a.mdActive {
				a.focusBlock(idx, m.Start-off, m.End-off)
			} else {
				a.selSyncStart, a.selSyncEnd = m.Start-off, m.End-off
			}
		}
	}

	style := a.resolveStyle(-1, dark, true)
	a.virt.beginFrame(a.liveScroll.Y)
	// 虚拟化列表：只构建视口内外的少数块，长文档的首帧与稳态帧成本与
	// 可见块数相关，而与文档总块数无关（编辑器仍按块工作，见 live_edit.go）。
	blockStarts := a.mdCacheOffsets
	// 快照 active：块内 Enter 可能在本轮构建中把 mdActive 挤到别的块，
	// 同一帧不能因此误建第二个编辑器（同 handle 冲突），重建交给下一帧。
	active := a.mdActive
	// 外层 Column 接收列表不报告的左右留白和行间空隙。纵坐标还能对上某一行时，
	// 光标落在那一行的点击位置；只有点在最后一块下方的纸面才去文末。
	wrap := ui.Column(c).Fill().Grow(1)
	var hits []liveHit
	wrap.OnClick(func() {
		if !a.liveClickHandled {
			a.focusNearestHit(c, hits, style.blockGap, true)
		}
	})
	wrap.Children(func() {
		list := ui.List(c, &a.liveList, len(blocks), func(i int) {
			blk := blocks[i]
			pad := listGapAfter(blocks, i, style.blockGap)
			if i == active {
				a.activeBlock(c, t, &hits, pad)
				return
			}
			start := blockStarts[i]
			blockEnd := blockStarts[i+1] - 1
			a.liveBlockRow(c, t, dark, i, blk, isLiveSeparator(blocks, i), start, blockEnd+1, &hits, pad)
		})
		a.virt.endFrame(list.Bounds())
		list.Fill().Background(a.paperBackground(style.background, dark, c.Vibrancy())).
			TrackScroll(&a.liveScroll).
			// MyGo Padding 按 CSS 序：先上下后左右；padX 是左右内边距。
			// 块间距不用 List 的统一 Gap：同一列表的相邻项要收紧，
			// 改由每行尾部 padding 按相邻块类型给出（listGapAfter）。
			Padding(style.padY, style.padX).
			TextColor(style.text).Font(style.bodyFont).FontSize(style.bodySize).LineHeight(style.lineHeight)
		if style.sketch {
			// 纸面画在滚动视口上而不是内容行上：内容比视口短时，下方空白同样是纸。
			dots := withAlpha(style.textMuted, 0.22)
			margin := withAlpha(style.accent, 0.38)
			marginX := style.padX * 0.45
			list.Draw(func(p *ui.Painter, r ui.Rect) {
				// 绘制时读最新滚动量：滚动可能只重绘不重建，点阵仍与文字同步。
				dotGrid(p, r, a.liveScroll.Y, dots)
				sketchLine(p, r.X+marginX, r.Y, r.X+marginX, r.Y+r.H, 1.2, margin, 3)
			})
		}
	})
	// 内层文字、输入框和表格已处理的点击保留自己的光标。
	// 只消费一次点击标记，避免影响下一次视口点击。
	a.liveClickHandled = false
}

// listGapAfter 返回块 i 与下一块之间应有的间距（画在块 i 的尾部 padding 里）：
//   - 最后一行为 0；
//   - 下一块是分隔空行时给两倍：分隔行自身 0 高（Height 是 border-box，padding 被裁掉），
//     原先它在统一 Gap 下贡献了两侧两个间隙；但列表/待办项尾随的分隔空行例外，
//     只给整块间距：空行隔开的是同一列表的延续组，两倍会让“组尾→下一项”比
//     普通块间距还宽，新加的待办像被孤立项。
//   - 同一列表的相邻项（列表/待办相连，源码中间无空行）收紧到 blockGap 的四分之一，
//     避免列表节距接近双倍行距；其余边界维持完整块间距。
func listGapAfter(blocks []liveBlock, i int, gap float32) float32 {
	if i < 0 || i >= len(blocks)-1 {
		return 0
	}
	if isLiveSeparator(blocks, i+1) {
		if sameListItem(blocks[i], blocks[i+2]) {
			return gap
		}
		return gap * 2
	}
	if sameListItem(blocks[i], blocks[i+1]) {
		return roundHalf(gap * 0.25)
	}
	return gap
}

// sameListItem 报告两个块是否属于同一个列表的相邻项。
func sameListItem(a, b liveBlock) bool {
	return (a.kind == bkList || a.kind == bkTask) && (b.kind == bkList || b.kind == bkTask)
}

// activeBlock keeps the block's rendered structure while editing visible contents.
func (a *app) activeBlock(c *ui.Context, t *ui.Theme, hits *[]liveHit, pad float32) {
	blk := a.mdBlocks()[a.mdActive]
	var projection liveProjection
	if blk.kind != bkTable {
		projection = a.previewProjection(blk)
	}
	if a.fenceDraft && isFenceDraftLine(blk.raw) {
		projection = projectRegion(blk.raw, 0, utf8.RuneCountInString(blk.raw), false, false)
		// 回车确认前保持原生输入框层级不变，避免重挂到代码卡片时重复输入或错位光标。
		blk.kind = bkPara
	}
	md := a.resolveStyle(-1, themeIsDark(t), true)
	row := ui.Row(c.Key("blk-"+strconv.Itoa(a.mdActive))).Gap(8).FillWidth().Padding(0, 0, pad, 0)
	// 文档级全选：焦点块与其余块呈现一致的选区底色。
	if a.selDoc {
		row.Background(t.Selection)
	}
	before := 0
	if hits != nil {
		before = len(*hits)
	}
	row.Children(func() {
		switch blk.kind {
		case bkTable:
			a.editTable(c, t, blk)
		case bkHeading:
			if md.sketch {
				sketchHeadingFrame(c, md, blk, projection.text).Children(func() {
					a.activeText(c, t, blk, projection, hits)
				})
			} else {
				a.activeText(c, t, blk, projection, hits)
			}
		case bkTask:
			a.taskCheckbox(c, blk, md)
			a.activeText(c, t, blk, projection, hits)
		case bkList:
			listMarker(c, md, blk)
			a.activeText(c, t, blk, projection, hits)
		case bkQuote:
			quoteBar(c, md, uint32(blk.start))
			a.activeText(c, t, blk, projection, hits)
		case bkCode:
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				codeCard(c, md, t, uint32(blk.start)).FillWidth().Children(func() {
					a.activeText(c, t, blk, projection, hits)
				})
				a.codeLangBadge(c, md, blk)
			})
		case bkRule:
			ui.Column(c).FillWidth().Children(func() {
				ruleLine(c, md, uint32(blk.start))
				a.activeText(c, t, blk, projection, hits)
			})
		default:
			a.activeText(c, t, blk, projection, hits)
		}
	})
	// 点在标记、引用条或行的留白上时，输入框没有接到这次点击。
	if hits != nil && len(*hits) > before {
		mine := append([]liveHit(nil), (*hits)[before:]...)
		row.OnClick(func() {
			a.liveClickHandled = true
			a.focusNearestHit(c, mine, 0, false)
		})
	}
}

func (a *app) activeText(c *ui.Context, t *ui.Theme, blk liveBlock, p liveProjection, hits *[]liveHit) {
	md := a.resolveStyle(-1, themeIsDark(t), true)
	if a.mdProjection.raw != p.raw || a.mdProjection.start != p.start || a.mdProjection.end != p.end || a.mdProjection.cell != p.cell {
		a.mdVisible = p.text
	}
	a.mdProjection = p
	beforeEdit := a.liveSnapshot()
	oldStart, oldEnd := p.visibleSelection(a.mdCaret, a.mdSelEnd)
	if a.selSyncStart >= 0 {
		oldStart, oldEnd = p.visibleSelection(a.selSyncStart, a.selSyncEnd)
	}

	a.captureClipboardService(c)
	ta := ui.TextAreaBase(c.Key("md-active"), &a.mdVisible).
		Bind(&a.editorHandle).ContextMenu(a.editorContextMenu).Label(locales.EditorContent())
	if a.selDoc {
		// 文档级全选：焦点块的输入框铺选区底色，避免自绘背景盖住色带。
		ta = ta.Background(t.Selection)
	}
	ta = ta.
		Lines(1, 4096).
		MinWidth(0).
		Font(md.bodyFont).FontSize(md.bodySize).LineHeight(md.lineHeight).
		FocusRing(false)
	ta = ta.FillWidth().Grow(1)
	if !a.findOpen && !a.linkTool.open {
		ta = ta.AutoFocus()
	}
	ta = ta.
		OnPaste(func(text string) bool {
			if text != "" {
				a.nextEditIsPaste = true
			}
			return false
		}).
		HandleInput(func(ev ui.InputEvent) bool { return a.recordProjectedKey(ev, p) })
	// 文本框是滚动容器的子控件；标记已处理，避免父容器把同一次点击
	// 当作空白点击，从而将光标重置到文末。
	if ta.Clicked() {
		a.liveClickHandled = true
	}
	if hits != nil {
		mdFont := bodyLiveFont(md)
		switch blk.kind {
		case bkHeading:
			mdFont = headingLiveFont(md, blk.level)
		case bkCode:
			mdFont = codeLiveFont(md)
		}
		recordLiveHit(ta, a.mdActive, p, mdFont, hits)
	}
	if a.liveFocus > 0 {
		a.liveFocus = 0
		a.editorHandle.Focus(c)
	}
	switch blk.kind {
	case bkHeading:
		styleHeading(ta, blk.level, md)
	case bkQuote:
		ta.TextColor(t.TextMuted)
	case bkCode:
		ta.Font(md.monoFont).FontSize(md.codeSize).LineHeight(md.codeLineHeigh)
	case bkTable:
		ta.FontSize(md.codeSize)
		if len(blk.table.rows) > 0 && p.start < a.previewTable(blk).positions[len(blk.table.rows[0])-1].end {
			ta.Bold()
		}
	}
	ranges := p.textRangesInBlock(a.highlightQuery(), t, md, blk, a.prefs.Appearance.CodeTheme)
	ranges = append(ranges, a.timeObjectRanges(c, md, blk, p)...)
	if len(p.objects) > 0 {
		a.paintTimeObjects(c, ta, md, blk)
		if hits != nil && len(*hits) > 0 {
			(*hits)[len(*hits)-1].spans = timeObjectSpans(p, p.textRanges(a.highlightQuery(), t, md), a.timeObjectRanges(c, md, blk, p))
		}
		ta.OnClick(func() {
			a.hitTimeObject(c.Resolve(a.editorHandle), blk)
		})
	}
	if a.selDoc {
		for i := range ranges {
			ranges[i].Background = ui.Color{}
		}
	}
	ta.TextRanges(ranges...)
	// 时间对象属于编辑器文字流，时间弹层锚在同一个编辑器上。
	if a.timePop.open && a.timePop.line == blk.start {
		a.timePopover(c, ta, blk)
	}

	// 原生选区使用可见偏移，文档管线使用源码偏移；IME 组合仍由控件维护。
	cs, ce := ta.TextSelection()
	wasComposing := a.prevComposing
	a.mdComposing = ta.Composing()
	a.prevComposing = a.mdComposing
	a.mdCaret, a.mdSelEnd = p.sourceOffset(cs), p.sourceOffset(ce)
	// 光标移动/换块后重新按当前位置计算待输入格式（意图清零）。
	if a.selSyncStart < 0 {
		a.maybeExpirePending()
	}

	if a.liveUndoPending != 0 {
		redo := a.liveUndoPending < 0
		a.liveUndoPending = 0
		a.undoLive(redo)
		c.Invalidate()
		return
	}
	defer func() { a.rememberLiveEdit(beforeEdit, a.takePendingEditKind()) }()

	if ta.Changed() && !a.mdComposing {
		if a.selDoc {
			// 文档级选区：输入文本不可在事件层拦截（走 NSTextInputClient），
			// 在这里把控件对整块可见文字的替换升级为对整个文档的替换。
			a.docSelectionReplace(a.mdVisible, p, cs)
			c.Invalidate()
			return
		}
		// 行尾键入 @：占位符就地转化为时间弹层（像表格输入的即时转换）。
		a.tryTimeTrigger(blk, &cs, &ce)
		// 原生编辑应把光标推进到新内容之后，但 AX 整值替换会保留旧光标；
		// 纯追加而光标仍停在前缀内时先吸附到末尾，否则下一个字符会插进中间。
		if cs == ce {
			if n := nativeCaretAfterEdit(p.text, a.mdVisible, cs); n != cs {
				cs, ce = n, n
			}
		}
		// 退化的未闭合围栏（整块只有一行围栏，如刚键入的 ```）：可编辑区域
		// 是围栏行末尾的零宽区间，直接插入会改写围栏行（变成语言标识）。
		// 换用带合成内容行的投影，让输入落到围栏之后。
		if p.start == p.end && p.end == utf8.RuneCountInString(p.raw) && p.end > 0 {
			if i := strings.LastIndexByte(p.raw, '\n'); isFenceOpeningLine(p.raw[i+1:]) {
				p = projectRegion(p.raw+"\n", p.end+1, p.end+1, false, false)
			}
		}
		raw, caret, wrapped := a.replaceProjectedInput(p, blk, a.mdVisible, cs, oldStart, oldEnd)
		kind := editInput
		if a.nextEditIsPaste {
			kind, a.nextEditIsPaste = editPaste, false
		} else if wasComposing && !a.mdComposing {
			kind = editIME // IME 一次确认作为一个事务
		}
		a.pendingEditKind = kind
		// 待输入格式：第一次输入/粘贴/IME 确认时生成有效 Markdown。
		if wrapped {
			a.pendingEditKind = editCommand
		}
		startFence := !a.fenceDraft && kind == editInput && strings.Trim(p.raw, " ") == "" && isFenceDraftLine(raw)
		clearFence := a.fenceDraft && !isFenceDraftLine(raw)
		if startFence {
			a.startFenceDraft(blk.start)
		}
		a.mdDraft = raw
		a.commitDraft(caret)
		if clearFence {
			a.clearFenceDraft()
		}
		if kind == editPaste {
			a.scrollActive = true
			a.liveList.ScrollIntoView(a.mdActive)
		}
		if a.selSyncStart < 0 {
			a.selSyncStart, a.selSyncEnd = caret, caret
		}
		a.onEdited()
		c.Invalidate()
		return
	}

	// 自动建表是一次可撤销的输入转换。
	if a.tableUndoPending {
		a.tableUndoPending = false
		if a.activeTab().Text == a.autoTableAfter {
			a.applyDocEdit(a.autoTableBefore, a.autoTableLine, a.autoTableCol)
			a.autoTableAfter = ""
			a.onEdited()
			c.Invalidate()
			return
		}
	}

	// 上一帧记录的边界键：在已发布的光标上验证执行。
	if a.keyHint != 0 {
		hint := a.keyHint
		a.keyHint = 0
		before, beforeDoc, beforeActive := a.mdDraft, a.activeTab().Text, a.mdActive
		a.applyProjectedHint(hint, p, cs, ce)
		if a.activeTab().Text != beforeDoc {
			a.onEdited()
		}
		// 表格编辑改变草稿后照常走重建流程（无早退返回）。
		if hint == ui.KeyEnter && a.mdDraft != before {
			c.Invalidate()
			return
		}
		if a.mdDraft != before || a.activeTab().Text != beforeDoc || a.mdActive != beforeActive ||
			(p.cell && a.selSyncStart >= 0 && (a.selSyncStart < p.start || a.selSyncStart > p.end)) {
			// 草稿/文档已改写：本轮的块列表与编辑器缓冲已过期，
			// 选区留到重建后再应用（构造时 setText 会重新同步缓冲）。
			c.Invalidate()
			return
		}
	}

	// 强制选区（切块/合块/搜索跳转后）。
	if a.selSyncStart >= 0 {
		s, e := a.selSyncStart, a.selSyncEnd
		a.selSyncStart, a.selSyncEnd = -1, -1
		if !a.mdComposing {
			vsStart, vsEnd := p.visibleSelection(s, max(e, s))
			ta.SetTextSelection(vsStart, vsEnd)
			vs, ve := ta.TextSelection()
			a.mdCaret, a.mdSelEnd = p.sourceOffset(vs), p.sourceOffset(ve)
		} else {
			a.selSyncStart, a.selSyncEnd = s, e
		}
	}

	if a.scrollActive {
		a.scrollActive = false
		ta.ScrollIntoView()
	}
}

// liveBlockRow 渲染一个非焦点块。点在文字上时按排版落光标；点在标记或行内留白上时，
// 用同一行的文字盒子换算。勾选框是更内层的可点元素，它的点击不会冒泡到行。
func (a *app) liveBlockRow(c *ui.Context, t *ui.Theme, dark bool, i int, blk liveBlock, separator bool, docStart, docEnd int, hits *[]liveHit, pad float32) {
	var row ui.Element
	if blk.kind == bkBlank {
		// 源码分隔符不占行高；额外的空段落可见、可点击，且一次退格只删一个。
		if separator {
			row = ui.Row(c.Key("blk-" + strconv.Itoa(i))).FillWidth().Height(0)
		} else {
			// 分隔行 0 高时 padding 被 border-box 裁掉，间距记在上一行尾部。
			row = ui.Row(c.Key("blk-"+strconv.Itoa(i))).FillWidth().Height(20).Padding(0, 0, pad, 0)
		}
		if !separator {
			row.OnClick(func() {
				a.liveClickHandled = true
				a.focusBlock(i, 0, 0)
			})
			if hits != nil {
				recordLiveHit(row, i, a.previewProjection(blk), ui.Font{}, hits)
			}
		}
	} else {
		before := 0
		if hits != nil {
			before = len(*hits)
		}
		row = ui.Row(c.Key("blk-"+strconv.Itoa(i))).Gap(8).FillWidth().Padding(0, 0, pad, 0)
		row.Children(func() { a.renderLiveBlock(c, t, dark, i, blk, hits) })
		// 时间弹层锚在整行上：MyGo 的 AttachTo 语义是“锚点上的按压不算面板外”，
		// 因此行内点击、聚焦、移动光标都不会关掉弹层；点其他块才关闭。
		if a.timePop.open && a.timePop.line == blk.start {
			a.timePopover(c, row, blk)
		}
		var mine []liveHit
		if hits != nil {
			mine = append([]liveHit(nil), (*hits)[before:]...)
		}
		if len(mine) == 0 && hits != nil {
			n := utf8.RuneCountInString(blk.raw)
			recordLiveHit(row, i, projectRegion(blk.raw, n, n, false, false), ui.Font{}, hits)
		}
		row.OnClick(func() {
			if a.liveClickHandled {
				return
			}
			a.liveClickHandled = true
			if len(mine) == 0 {
				n := utf8.RuneCountInString(blk.raw)
				a.focusBlock(i, n, n)
				return
			}
			a.focusNearestHit(c, mine, 0, false)
		})
	}
	// 文档级全选：非焦点块画选区底色，提示它在选区内。
	if a.blockSelectionBand(docStart, docEnd) {
		row.Background(t.Selection)
	}
}

// renderLiveBlock 输出一个非焦点块的排版内容（列表现有 Row 内）。
func (a *app) renderLiveBlock(c *ui.Context, t *ui.Theme, dark bool, i int, blk liveBlock, hits *[]liveHit) {
	md := a.resolveStyle(-1, dark, true)
	if blk.hasTime {
		a.renderTimeBlock(c, t, md, i, blk, hits)
		return
	}
	switch blk.kind {
	case bkTable:
		a.tableGrid(c, t, md, blk, false)
	case bkHeading:
		proj := a.previewProjection(blk)
		font := headingLiveFont(md, blk.level)
		if md.sketch {
			sketchHeadingFrame(c, md, blk, proj.text).Children(func() {
				h := proj.searchContent(c, t, md, a.highlightQuery()).TextColor(t.Text)
				styleHeading(h, blk.level, md)
				a.bindPreviewClick(h, i, proj, font, hits)
			})
		} else {
			h := proj.searchContent(c, t, md, a.highlightQuery()).TextColor(t.Text)
			if !blk.hasTime {
				h = h.Grow(1)
			}
			styleHeading(h, blk.level, md)
			a.bindPreviewClick(h, i, proj, font, hits)
		}
	case bkRule:
		ruleLine(c, md, uint32(blk.start))
	case bkQuote:
		proj := a.previewProjection(blk)
		quoteBar(c, md, uint32(blk.start))
		var quote ui.Element
		if a.searchHitsBlock(blk, proj) {
			quote = proj.searchContent(c, t, md, a.highlightQuery()).TextColor(t.TextMuted)
		} else {
			quote = ui.RichText(c).TextColor(t.TextMuted)
			quote.Children(func() {
				renderInline(c, t, md, blk.text, blk.spans)
			})
		}
		if !blk.hasTime {
			quote = quote.Grow(1)
		}
		a.bindPreviewClick(quote, i, proj, bodyLiveFont(md), hits)
	case bkCode:
		a.renderCodePreview(c, t, md, i, blk, hits)
	case bkList:
		proj := a.previewProjection(blk)
		listMarker(c, md, blk)
		var text ui.Element
		if a.searchHitsBlock(blk, proj) {
			text = proj.searchContent(c, t, md, a.highlightQuery())
		} else {
			text = inlineContent(c, t, md, blk.text, blk.spans)
		}
		if !blk.hasTime {
			text = text.Grow(1)
		}
		a.bindPreviewClick(text, i, proj, bodyLiveFont(md), hits)
	case bkTask:
		proj := a.previewProjection(blk)
		a.taskCheckbox(c, blk, md)
		text := proj.searchContent(c, t, md, a.highlightQuery())
		if !blk.hasTime {
			text = text.Grow(1)
		}
		a.bindPreviewClick(text, i, proj, bodyLiveFont(md), hits)
	default:
		proj := a.previewProjection(blk)
		text := proj.searchContent(c, t, md, a.highlightQuery())
		if !blk.hasTime {
			text = text.Grow(1)
		}
		a.bindPreviewClick(text, i, proj, bodyLiveFont(md), hits)
	}
}

// searchHitsBlock 报告单行引用/列表块是否含查找命中。这两类块平时按切块时
// 预计算的行内片段渲染；多行块的投影会保留续行标记，只在单行时改走投影。
func (a *app) searchHitsBlock(blk liveBlock, proj liveProjection) bool {
	q := a.highlightQuery()
	return q != "" && !strings.Contains(blk.raw, "\n") && len(findMatches(proj.text, q)) > 0
}

// renderCodePreview 把代码块的每一行铺满卡片宽度。点在短行右侧空白时，
// 光标停在这一行，而不是整个代码块的末尾。
func (a *app) renderCodePreview(c *ui.Context, t *ui.Theme, md resolvedStyle, block int, blk liveBlock, hits *[]liveHit) {
	proj := a.previewProjection(blk)
	codeCard(c, md, t, uint32(blk.start)).Fill().Children(func() {
		cp, codeOn := codePaletteFor(a.prefs.Appearance.CodeTheme, t)
		var spans [][]ui.Span
		if codeOn {
			spans = a.previewCode(blk, cp)
		}
		lines := blk.lines
		shown := lines
		if len(shown) == 0 {
			shown = []string{" "}
		}
		font := codeLiveFont(md)
		// 行首源码偏移：虚拟化时跳过的行也要算进去。
		starts := make([]int, len(shown))
		off := proj.start
		for i := range shown {
			starts[i] = off
			if i < len(lines) {
				off += utf8.RuneCountInString(lines[i]) + 1
			}
		}
		est := md.codeSize * md.codeLineHeigh
		a.virtualRows(c, virtualRowsKey(blk.raw, bkCode, md), len(shown), est, -1, func(i int) ui.Element {
			source := ""
			if i < len(lines) {
				source = lines[i]
			}
			n := utf8.RuneCountInString(source)
			lineStart := starts[i]
			display := shown[i]
			if display == "" {
				display = " "
			}
			var el ui.Element
			if codeOn && i < len(spans) {
				el = ui.RichText(c.Key("cl-"+strconv.Itoa(i)), spans[i]...)
			} else {
				el = ui.Text(c.Key("cl-"+strconv.Itoa(i)), display)
			}
			el.Font(md.monoFont).FontSize(md.codeSize).LineHeight(md.codeLineHeigh).FillWidth()
			el.Selectable().OnClick(func() {
				a.liveClickHandled = true
				if source == "" {
					a.focusBlock(block, lineStart, lineStart)
					return
				}
				s, e := el.TextSelection()
				s, e = clamp(s, 0, n), clamp(e, 0, n)
				a.focusBlock(block, lineStart+s, lineStart+e)
			})
			if hits != nil {
				recordLiveHit(el, block, plainLineProjection(blk.raw, source, lineStart), font, hits)
			}
			return el
		})
	})
}

// inlineContent 输出段落文字：无行内标记时用普通 Text（可被命中测试/测试
// 工具找到），有标记时用 RichText 混排。spans 用切分时预计算的缓存。
func inlineContent(c *ui.Context, t *ui.Theme, md resolvedStyle, text string, spans []mSpan) ui.Element {
	if len(spans) == 0 {
		return ui.Text(c, text)
	}
	rt := ui.RichText(c)
	rt.Children(func() { renderInline(c, t, md, text, spans) })
	return rt
}

// renderInline 在当前 RichText 段落内输出带样式的行内片段。
// spans 是切分时预计算的行内标记（勿传 nil 渲染带标记的文本）。
func renderInline(c *ui.Context, t *ui.Theme, md resolvedStyle, text string, spans []mSpan) {
	if len(spans) == 0 {
		ui.Text(c, text)
		return
	}
	prev := 0
	rs := []rune(text)
	for _, sp := range spans {
		if sp.start > prev {
			ui.Text(c, string(rs[prev:sp.start]))
		}
		styled := inlineStyle(sp.kind, t, md)
		styled.Text = spanText(text, sp)
		ui.RichText(c, styled)
		prev = sp.end
	}
	if prev < len(rs) {
		ui.Text(c, string(rs[prev:]))
	}
}
