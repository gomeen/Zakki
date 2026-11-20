package app

// AI 建议的应用：insert / replace 走行级替换 + 独立撤销事务（⌘Z 可退）；
// newTab 追加标签写入内容（不切换、不可撤销，toast 如实说明）。应用前
// 对锚点做唯一性复查——审阅期间正文可能又变了。

import (
	"strings"
	"unicode/utf8"

	"zakki/internal/ai"
	"zakki/internal/markdown"
	"zakki/locales"
)

// aiApplyAll 依序应用全部待审建议；一条失败即停（剩余保留待审）。
func (a *app) aiApplyAll() {
	for len(a.ai.bar.proposals) > 0 {
		if !a.aiApplyIndex(0) {
			return
		}
	}
}

// aiApplyIndex 应用第 i 条建议；返回是否成功。
func (a *app) aiApplyIndex(i int) bool {
	if i < 0 || i >= len(a.ai.bar.proposals) {
		return false
	}
	p := a.ai.bar.proposals[i]
	if p.Kind == ai.KindNewTab {
		return a.aiApplyNewTab(p, i)
	}
	return a.aiApplyEdit(p, i)
}

// aiApplyEdit 应用文本编辑类建议：runAsCommand 提供撤销事务与焦点恢复。
func (a *app) aiApplyEdit(p ai.Proposal, idx int) bool {
	doc := a.activeTab().Text
	res, err := a.aiSplicePlan(p, doc)
	if err != nil {
		a.showToast(locales.AiApplyAnchorLost())
		return false
	}
	a.runAsCommand(func() docResult { return res })
	a.aiProposalDone(idx)
	a.showToast(locales.AiAppliedUndo())
	return true
}

// aiApplyNewTab 追加标签写入内容；不切换、不可 ⌘Z（撤销历史属于当前标签）。
func (a *app) aiApplyNewTab(p ai.Proposal, idx int) bool {
	if len(a.tabs) >= MaxTabCount {
		a.showToast(locales.TabMaxCannotAdd(MaxTabCount))
		return false
	}
	a.tabs = append(a.tabs, tab{ID: a.nextID(), Name: p.NewTab.Name, Text: p.NewTab.Content, Color: a.unusedTabColor()})
	a.afterTabsChanged(false)
	a.aiProposalDone(idx)
	a.showToast(locales.AiNewTabToast(p.NewTab.Name))
	return true
}

// aiSplicePlan 把建议换算成一次文档变换：insert 定位插入行，replace 定位
// 锚点所在行区间；锚点在当前正文里必须仍恰好出现一次。
func (a *app) aiSplicePlan(p ai.Proposal, doc string) (docResult, error) {
	lines := strings.Split(doc, "\n")
	switch p.Kind {
	case ai.KindReplace:
		startOff := strings.Index(doc, p.Anchor)
		if startOff < 0 || strings.Count(doc, p.Anchor) != 1 {
			return docResult{}, ai.ErrAnchorNotFound
		}
		startLine := lineOfByteOffset(lines, startOff)
		endLine := lineOfByteOffset(lines, startOff+len(p.Anchor))
		insert := strings.Join(p.Lines, "\n")
		newDoc := a.spliceDoc(doc, startLine, endLine, insert)
		return aiDocResult(doc, newDoc, startLine, insert), nil
	case ai.KindInsert:
		text := strings.Join(p.Lines, "\n")
		var afterLine int
		switch p.At {
		case ai.AtDocEnd:
			afterLine = len(lines) - 1
		case ai.AtListEnd:
			afterLine = a.aiListEndLine(lines)
		case ai.AtAfterAnchor:
			off := strings.Index(doc, p.Anchor)
			if off < 0 || strings.Count(doc, p.Anchor) != 1 {
				return docResult{}, ai.ErrAnchorNotFound
			}
			afterLine = lineOfByteOffset(lines, off+len(p.Anchor)-1)
		default:
			afterLine = len(lines) - 1
		}
		return fromMD(markdown.ApplyInsertAfterLine(doc, afterLine, text, utf8.RuneCountInString(text))), nil
	}
	return docResult{}, ai.ErrAnchorNotFound
}

// aiListEndLine 最后一个列表块（- / 数字 / 待办）的末行；没有列表回退文档末尾。
func (a *app) aiListEndLine(lines []string) int {
	last := len(lines) - 1
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		s := strings.TrimLeft(lines[i], " \t")
		if t == "" {
			continue
		}
		if listMarkerLen(s) > 0 || taskPrefixLen(s) > 0 {
			return i
		}
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") {
			continue // 标题/引用不阻断向前找列表
		}
		return last
	}
	return last
}

// aiDocResult 组装 replace 的变换结果：光标落在插入内容末尾。
func aiDocResult(oldDoc, newDoc string, startLine int, insert string) docResult {
	byteOff := lineByteOffset(newDoc, startLine) + len(insert)
	if byteOff > len(newDoc) {
		byteOff = len(newDoc)
	}
	caret := utf8.RuneCountInString(newDoc[:byteOff])
	return docResult{doc: newDoc, selStart: caret, selEnd: caret, changed: newDoc != oldDoc}
}

// lineOfByteOffset 返回字节偏移所在行（lines 为文档行切分）。
func lineOfByteOffset(lines []string, off int) int {
	acc := 0
	for i, l := range lines {
		acc += len(l) + 1
		if off < acc {
			return i
		}
	}
	return len(lines) - 1
}
