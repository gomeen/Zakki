package app

// 助理条视图：底部输入行 + 建议卡堆栈。卡片无边框：头行是语义色圆点 +
// 墨色徽标 + 灰色落点提示，diff 行用左侧 2px 语义色条 + 极淡底色
//（删除块红墨删除线）；卡间与输入行之间用 1px 直细线分组（与规划稿
// 一致，不用波浪线）。文案经 locales.*。

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"zakki/internal/ai"
	"zakki/internal/markdown"
	"zakki/locales"
)

// aiBarOverlay 底部居中的浮动助理面板：悬浮在正文上方、状态栏之上，
// 不占正文布局。宽度上限 520，卡堆栈超高时内部滚动。显隐由底栏 ✦ /
// ⌘⇧A / 菜单切换，Esc 也可关闭。
func (a *app) aiBarOverlay(c *ui.Context, t *ui.Theme, dark bool) {
	if !a.ai.bar.open {
		return
	}
	style := a.resolveStyle(a.active, dark, true)
	w, h := c.Size()
	panelW := min(float32(520), w-32)
	// 卡堆栈高度上限：留出顶栏、状态栏、输入行与呼吸空间。
	maxCards := min(max(h-240, float32(120)), float32(400))
	ui.Overlay(c, func() {
		// 纯输入态输入行贴满面板（零内边距、圆角与面板同 14）；
		// 出现建议卡/错误行/思考行时才用内边距分隔。
		solo := a.ai.bar.phase == aiBarInput && a.ai.bar.errText == ""
		panel := ui.Column(c.Key("ai-bar")).Width(panelW).Gap(7)
		if solo {
			panel.Radius(14)
		} else {
			panel.Gap(7).Padding(10, 12).Radius(14)
		}
		panel.Background(style.surface)
		panel.Absolute().Left((w - panelW) / 2).Bottom(statusBarHeight + 10)
		panel.Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.22))
		panel.Children(func() {
			if a.ai.bar.phase == aiBarReview {
				// 模型的回答/说明：常驻在卡片上方；纯问答时没有卡片，
				// 只有这一行 + 输入行。圆点标记与建议卡头行同语言。
				if a.ai.bar.answer != "" {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Width(6).Height(6).Radius(3).Background(style.ai.addInk).Shrink(0)
						ui.Text(c, a.ai.bar.answer).FontSize(12).TextColor(t.Text).
							Wrap().Grow(1).MinWidth(0)
					})
				}
				if len(a.ai.bar.proposals) > 0 {
					ui.Scroll(c.Key("ai-cards-scroll")).FillWidth().MaxHeight(maxCards).Children(func() {
						ui.Column(c).FillWidth().Gap(8).Children(func() {
							a.aiCards(c, t, style)
						})
					})
				}
			}
			if a.ai.bar.phase == aiBarThinking {
				// 思考态不加 ✦——下方输入行已有同款图标，重复；
				// 只留灰字，与回答行的克制一致。
				ui.Text(c, locales.AiBarThinking()).FontSize(12).TextColor(t.TextMuted)
			}
			if a.ai.bar.phase == aiBarInput && a.ai.bar.errText != "" {
				a.aiErrorRow(c, t)
			}
			if a.ai.bar.phase == aiBarReview {
				// 规划稿的 1px 直细线：比波浪线轻，两种主题一致。
				ui.Box(c).FillWidth().Height(1).Background(style.border.Mix(style.surface, 0.35))
			}
			a.aiInputRow(c, t, style)
		})
	})
}

// aiErrorRow 行内错误：未配置时给「去设置」，其余给「重试」。
func (a *app) aiErrorRow(c *ui.Context, t *ui.Theme) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.ai.bar.errText).FontSize(12).TextColor(t.Danger).Grow(1).MinWidth(0).Wrap()
		if !a.aiConfigured() {
			if ui.Button(c.Key("ai-open-settings"), locales.AiBarOpenSettings()).Clicked() {
				a.openSettings(pageAI)
			}
		} else if a.ai.bar.lastInstruction != "" {
			if ui.Button(c.Key("ai-retry"), locales.AiBarRetry()).Clicked() {
				a.aiSend(a.ai.bar.lastInstruction)
			}
		}
	})
}

// aiInputRow 输入行：加大的圆角输入框（含 ✦ 图标），Enter 发送；
// 聚焦不改变外观（边框恒定）。追问态换占位文案。
func (a *app) aiInputRow(c *ui.Context, t *ui.Theme, style resolvedStyle) {
	placeholder := locales.AiBarPlaceholder()
	if a.ai.bar.phase == aiBarReview && len(a.ai.bar.proposals) > 0 {
		placeholder = locales.AiBarFollowUp()
	}
	input := ui.Row(c).FillWidth().Height(38).PaddingX(10).Gap(7).Radius(10).
		AlignItems(ui.Center).Background(t.Background)
	input.Children(func() {
		ui.Icon(c, iconSparkles).Size(15, 15).TextColor(style.accent).Shrink(0)
		in := ui.TextInputBase(c.Key("ai-input"), &a.ai.bar.input).
			Label(locales.AiBarTitle()).Placeholder(placeholder).
			FontSize(14).
			Grow(1).MinWidth(40).FocusBind(&a.ai.bar.focus, 1).
			HandleInput(func(ev ui.InputEvent) bool { return a.aiInputKey(ev) })
		a.ai.bar.composing = in.Composing()
		if in.Submitted() {
			a.aiSend(a.ai.bar.input)
		}
	})
}

// aiInputKey 输入框按键：Esc 走统一的助理条退出链；组合中的按键归输入法。
func (a *app) aiInputKey(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown || a.ai.bar.composing {
		return false
	}
	if ev.Key == ui.KeyEscape && ev.Mods == 0 {
		a.aiEsc()
		return true
	}
	return false
}

// aiEsc 助理条的 Esc 语义：先退出原文编辑，再取消在途请求，最后关闭整条。
func (a *app) aiEsc() {
	switch {
	case a.ai.bar.editing >= 0:
		a.ai.bar.editing = -1
	case a.ai.bar.phase == aiBarThinking && a.ai.bar.cancel != nil:
		a.ai.bar.cancel()
		a.ai.bar.phase = aiBarInput // 交付回调会把阶段复位；这里先退回输入态避免卡在思考
	default:
		a.closeAIBar()
		return
	}
	a.invalidateMain()
}

// aiCards 建议卡堆栈：多卡时提供「全部应用」，最多渲染 4 张。
func (a *app) aiCards(c *ui.Context, t *ui.Theme, style resolvedStyle) {
	if n := len(a.ai.bar.proposals); n > 1 {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.AiBarProposals(n)).FontSize(12).TextColor(t.TextMuted).Grow(1)
			if ui.Button(c.Key("ai-apply-all"), locales.AiBarApplyAll()).Clicked() {
				a.aiApplyAll()
			}
		})
	}
	for i := range a.ai.bar.proposals {
		if i >= 4 {
			ui.Text(c, locales.AiBarMoreProposals(len(a.ai.bar.proposals)-4)).
				FontSize(12).TextColor(t.TextMuted)
			break
		}
		if i > 0 {
			// 卡间分隔：与规划稿一致的 1px 直细线。
			ui.Box(c).FillWidth().Height(1).Background(style.border.Mix(style.surface, 0.35))
		}
		a.aiCard(c, t, style, i)
	}
}

// aiCard 单张建议卡：无边框的安静卡片——语义色圆点 + 墨色徽标标头、
// 左色条淡底预览、墨底主按钮操作区；多卡之间由 aiCards 画分隔线。
func (a *app) aiCard(c *ui.Context, t *ui.Theme, style resolvedStyle, i int) {
	p := a.ai.bar.proposals[i]
	card := ui.Column(c.Key("ai-card-" + strconv.Itoa(i))).FillWidth().Gap(7)
	card.Children(func() {
		// 头行：绿色圆点（净新增）+ 墨色徽标 + 灰色落点提示；强调色
		// 只留给数据与 ✦，不再染标题。
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(style.ai.addInk).Shrink(0)
			ui.Text(c, aiBadgeText(p)).FontSize(12).Bold().TextColor(style.text).Grow(1).MinWidth(0)
			ui.Text(c, aiWhereText(p)).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		})
		if a.ai.bar.editing == i {
			a.aiCardEditor(c, t, style, i)
		} else {
			aiCardPreview(c, t, style, p)
			if n := aiAlertNote(p); n != "" {
				ui.Text(c, n).FontSize(11).TextColor(t.TextMuted)
			}
			// 操作区：墨底主按钮写明去向（强调色不染按钮），「放弃」
			// 退到右端低层，远离高频点击区。
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				apply := ui.ButtonBase(c.Key("ai-apply")).Padding(5, 14).Radius(8).
					Background(style.text).Cursor(ui.CursorPointer).FocusRing(false)
				apply.Children(func() { ui.Text(c, locales.AiBarApply()).FontSize(12).Bold().TextColor(style.background) })
				if apply.Clicked() {
					a.aiApplyIndex(i)
				}
				edit := ui.ButtonBase(c.Key("ai-edit")).Padding(5, 10).Radius(8).Cursor(ui.CursorPointer)
				edit.Children(func() { ui.Text(c, locales.AiBarEdit()).FontSize(12).TextColor(t.TextMuted) })
				edit.OnClick(func() { a.aiToggleEdit(i) })
				ui.Box(c).Grow(1)
				dismiss := ui.ButtonBase(c.Key("ai-dismiss")).Padding(5, 10).Radius(8).Cursor(ui.CursorPointer)
				dismiss.Children(func() { ui.Text(c, locales.AiBarDismiss()).FontSize(13).TextColor(t.TextMuted) })
				dismiss.OnClick(func() { a.aiDismiss(i) })
			})
		}
	})
}

// aiCardEditor 原文编辑态：多行 Markdown 输入（TextArea）+ 完成/取消。
func (a *app) aiCardEditor(c *ui.Context, t *ui.Theme, style resolvedStyle, i int) {
	ta := ui.TextAreaBase(c.Key("ai-edit-draft"), &a.ai.bar.editDraft).
		Label(locales.AiBarEdit()).FillWidth().MinHeight(52).MaxHeight(160).
		FontSize(12).Padding(6, 8).Radius(8).
		Background(t.Background).Border(1, t.Border)
	_ = ta
	ui.Row(c).Gap(6).Children(func() {
		done := ui.ButtonBase(c.Key("ai-edit-done")).Padding(5, 14).Radius(8).
			Background(style.text).Cursor(ui.CursorPointer).FocusRing(false)
		done.Children(func() { ui.Text(c, locales.AiBarEditDone()).FontSize(12).Bold().TextColor(style.background) })
		if done.Clicked() {
			a.aiCommitEdit()
		}
		cancel := ui.ButtonBase(c.Key("ai-edit-cancel")).Padding(5, 10).Radius(8).Cursor(ui.CursorPointer)
		cancel.Children(func() { ui.Text(c, locales.AiBarEditCancel()).FontSize(13).TextColor(t.TextMuted) })
		cancel.OnClick(func() {
			a.ai.bar.editing = -1
			a.invalidateMain()
		})
	})
}

// aiCardPreview 建议内容预览：replace 先展示锚点原文（删除块），再展示新行
// （新增块）；insert 只展示新增块；newTab 展示标签名与内容首几行。
func aiCardPreview(c *ui.Context, t *ui.Theme, style resolvedStyle, p ai.Proposal) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		switch p.Kind {
		case ai.KindNewTab:
			aiNewTabPreview(c, style, p)
		case ai.KindReplace:
			aiDiffBlock(c, style, false, p.Anchor, false)
			for _, line := range p.Lines {
				aiDiffBlock(c, style, true, line, isTaskLine(line))
			}
		default:
			for _, line := range p.Lines {
				aiDiffBlock(c, style, true, line, isTaskLine(line))
			}
		}
	})
}

// isTaskLine 是否 - [ ] / - [x] 待办行。
func isTaskLine(line string) bool {
	s := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(s, "- [ ]") || strings.HasPrefix(s, "- [x]")
}

// aiDiffBlock 单行 diff：左侧 2px 语义色条 + 极淡底色，正文保持墨色
// （删除行红墨删除线）；待办行加方框并加粗，行尾时间令牌为中性胶囊。
func aiDiffBlock(c *ui.Context, style resolvedStyle, add bool, line string, task bool) {
	ink, bg := style.ai.addInk, style.ai.addBg
	if !add {
		ink, bg = style.ai.delInk, style.ai.delBg
	}
	blk := ui.Box(c).FillWidth().Radius(6).Padding(3, 8).Background(bg)
	blk.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, ink, 0)
	})
	blk.Children(func() {
		ui.RichText(c).Children(func() {
			text := line
			if task {
				ui.RichText(c, ui.Span{Text: "☐ ", Color: ink, Size: 13})
				s := strings.TrimLeft(line, " \t")
				text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "- [ ]"), "- [x]"))
			}
			body, chip, has := aiSplitTimeToken(text)
			span := ui.Span{Text: body, Color: style.text}
			if task {
				span.Size, span.Weight = 13, 600
			}
			if !add {
				span.Color, span.Strikethrough, span.DecorationColor = ink, true, ink
			}
			ui.RichText(c, span)
			if has {
				ui.RichText(c, ui.Span{Text: "  " + chip, Color: style.text, Size: 11,
					Background: style.border.Mix(style.surface, 0.45), Radius: 5})
			}
		})
	})
}

// aiNewTabPreview 新建标签建议预览：标签名 + 内容首几行（左色条淡底）。
func aiNewTabPreview(c *ui.Context, style resolvedStyle, p ai.Proposal) {
	blk := ui.Box(c).FillWidth().Radius(6).Padding(5, 9).Background(style.ai.addBg)
	blk.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, style.ai.addInk, 0)
	})
	blk.Children(func() {
		ui.Text(c, "# "+p.NewTab.Name).FontSize(13).Bold().TextColor(style.text)
		n := 0
		for _, line := range strings.Split(strings.TrimRight(p.NewTab.Content, "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if n++; n > 4 {
				ui.Text(c, "…").FontSize(12).TextColor(style.textMuted)
				break
			}
			ui.Text(c, line).FontSize(12).TextColor(style.textMuted)
		}
	})
}

// aiSplitTimeToken 把行尾时间令牌簇拆出来单独展示。粗校验避免把邮箱等
// 误当令牌：@ 前必须是空白或行首，令牌段不含中文标点。
func aiSplitTimeToken(line string) (body, chip string, has bool) {
	idx := strings.LastIndexFunc(line, func(r rune) bool { return r == '@' })
	if idx < 0 {
		return line, "", false
	}
	if idx > 0 && line[idx-1] != ' ' && line[idx-1] != '\t' {
		return line, "", false
	}
	tok := strings.TrimSpace(line[idx:])
	if len(tok) < 2 || strings.ContainsAny(tok, "，。；：、") {
		return line, "", false
	}
	return strings.TrimRight(line[:idx], " \t"), strings.TrimPrefix(tok, "@"), true
}

// aiBadgeText 建议徽标：类型 + 模型摘要。
func aiBadgeText(p ai.Proposal) string {
	switch p.Kind {
	case ai.KindReplace:
		return locales.AiBadgeReplace(p.Summary)
	case ai.KindNewTab:
		return locales.AiBadgeNewTab(p.Summary)
	default:
		return locales.AiBadgeInsert(p.Summary)
	}
}

// aiWhereText 落点说明。
func aiWhereText(p ai.Proposal) string {
	switch {
	case p.Kind == ai.KindReplace:
		return locales.AiWhereReplace()
	case p.Kind == ai.KindNewTab:
		return locales.AiWhereNewTab(p.NewTab.Name)
	case p.At == ai.AtListEnd:
		return locales.AiWhereListEnd()
	case p.At == ai.AtAfterAnchor:
		return locales.AiWhereAfterAnchor()
	default:
		return locales.AiWhereDocEnd()
	}
}

// aiAlertNote 提醒类建议的说明行：解析新行里的时间令牌，给出「将于 … 提醒」。
func aiAlertNote(p ai.Proposal) string {
	if p.Kind == ai.KindNewTab {
		return ""
	}
	for _, line := range p.Lines {
		if tok, ok := markdown.FindTimeToken(line); ok && !tok.Value.Time.IsZero() {
			return locales.AiAlertNote(tok.Value.Time.Format("2006-01-02 15:04"))
		}
	}
	return ""
}
