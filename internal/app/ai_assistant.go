package app

// AI 助理条会话：一句指令 → 后台 Ask → 结构化建议卡待审 → 确认应用。
// 状态只在主线程改；后台 goroutine 只拿主线程拍下的快照（指令、标签名
// 与文本、客户端、追问上下文），完成经 runOnMain 回 aiDeliver。等待期间
// 用户编辑会使指纹失效，交付时丢弃建议并提示。

import (
	"context"
	"errors"
	"strings"

	"zakki/internal/ai"
	"zakki/locales"
)

// aiBarPhase 助理条阶段。
type aiBarPhase int

const (
	aiBarInput    aiBarPhase = iota // 待输入（或显示行内错误）
	aiBarThinking                   // 请求进行中
	aiBarReview                     // 待审建议
)

// aiBarState 助理条会话状态（主线程，嵌在 aiAssistState 里）。
type aiBarState struct {
	open      bool
	input     string // 输入框工作副本
	phase     aiBarPhase
	focus     int // FocusBind 焦点请求：1 = 输入框
	composing bool
	errText   string // 行内错误（已本地化）

	// 会话：最近一轮指令与建议（追问时作为上下文重发）。
	lastInstruction string
	followUp        *ai.FollowUp
	proposals       []ai.Proposal
	editing         int    // 正在编辑原文的建议下标；-1 无
	editDraft       string // 编辑框工作副本（多行 Markdown）

	// 在途请求（主线程持有 cancel；seq 递增使旧结果作废）。
	cancel context.CancelFunc
	reqSeq int
	tabID  uint64 // 发起时的标签：切换标签即作废
	docFP  string // 发起时的全文：交付时比对，变化即失效
}

// aiAsker 是 Ask 的测试注入面；生产实现是 *ai.Client。
type aiAsker interface {
	Ask(context.Context, ai.AskRequest) (ai.AskResult, error)
}

// invalidateMain 请求主窗口重绘（窗口未开时什么都不做）。
func (a *app) invalidateMain() {
	if a.win != nil && a.winOpen {
		a.win.Update(func() {})
	}
}

// openAIBar 打开助理条并聚焦输入框；已打开时只聚焦。
func (a *app) openAIBar() {
	if a.ai.bar.open {
		a.ai.bar.focus = 1
		a.invalidateMain()
		return
	}
	a.ai.bar = aiBarState{open: true, editing: -1, focus: 1}
	a.invalidateMain()
}

// closeAIBar 关闭助理条：取消在途请求并丢弃待审建议（Esc / 切标签共用）。
func (a *app) closeAIBar() {
	if a.ai.bar.cancel != nil {
		a.ai.bar.cancel()
	}
	a.ai.bar = aiBarState{editing: -1}
	a.invalidateMain()
}

// resetAIBar 切标签时丢弃待审建议与在途请求：建议只对发起时的标签有效。
func (a *app) resetAIBar() {
	if a.ai.bar.open || a.ai.bar.cancel != nil {
		a.closeAIBar()
	}
}

// aiSend 发送（或追问）一条指令。追问 = 上一轮建议作为上下文整组重发。
func (a *app) aiSend(instruction string) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || a.ai.bar.phase == aiBarThinking {
		return
	}
	if !a.aiConfigured() {
		a.ai.bar.errText = locales.AiErrNotConfigured()
		a.invalidateMain()
		return
	}
	// 预览态先把焦点块草稿并入正文，快照取到完整文本。
	a.ensureDraftSynced()
	tab := a.activeTab()
	var client aiAsker = a.aiClient()
	if a.ai.asker != nil { // 测试注入
		client = a.ai.asker
	}
	if a.ai.bar.cancel != nil {
		a.ai.bar.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	seq := a.ai.bar.reqSeq + 1
	follow, now := a.ai.bar.followUp, a.now()
	name, doc := tab.Name, tab.Text
	a.ai.bar.cancel = cancel
	a.ai.bar.reqSeq = seq
	a.ai.bar.phase = aiBarThinking
	a.ai.bar.errText = ""
	a.ai.bar.input = ""
	a.ai.bar.editing = -1
	a.ai.bar.lastInstruction = instruction
	a.ai.bar.tabID = tab.ID
	a.ai.bar.docFP = doc
	a.invalidateMain()
	go func() {
		res, err := client.Ask(ctx, ai.AskRequest{
			Instruction: instruction, TabName: name, NoteMarkdown: doc,
			Now: now, FollowUp: follow,
		})
		a.runOnMain(func() { a.aiDeliver(seq, instruction, res, err) })
	}()
}

// aiDeliver 处理一次请求的完成（主线程）；被更新请求取代的旧结果作废。
func (a *app) aiDeliver(seq int, instruction string, res ai.AskResult, err error) {
	if seq != a.ai.bar.reqSeq {
		return
	}
	a.ai.bar.cancel = nil
	if err != nil {
		if errors.Is(err, ai.ErrCancelled) {
			// 用户取消：回到待输入；上一轮建议还在则回到待审。
			a.ai.bar.phase = aiBarInput
			if len(a.ai.bar.proposals) > 0 {
				a.ai.bar.phase = aiBarReview
			}
			a.invalidateMain()
			return
		}
		a.ai.bar.phase = aiBarInput
		a.ai.bar.errText = aiErrText(err)
		a.ai.bar.focus = 1
		a.invalidateMain()
		return
	}
	// 指纹失效：等待期间正文或标签变了，建议不再可靠。
	if a.ai.bar.tabID != a.activeTab().ID || a.activeTab().Text != a.ai.bar.docFP {
		a.ai.bar.phase = aiBarInput
		a.ai.bar.followUp = nil
		a.ai.bar.proposals = nil
		a.ai.bar.errText = locales.AiStaleNote()
		a.ai.bar.focus = 1
		a.invalidateMain()
		return
	}
	a.ai.bar.proposals = res.Proposals
	a.ai.bar.followUp = &ai.FollowUp{Instruction: instruction, Result: res}
	a.ai.bar.phase = aiBarReview
	a.invalidateMain()
}

// aiDismiss 放弃第 i 条建议；全部处理完则回到待输入。
func (a *app) aiDismiss(i int) {
	if i < 0 || i >= len(a.ai.bar.proposals) {
		return
	}
	a.ai.bar.proposals = append(a.ai.bar.proposals[:i], a.ai.bar.proposals[i+1:]...)
	a.ai.bar.editing = -1
	if len(a.ai.bar.proposals) == 0 {
		a.ai.bar.phase = aiBarInput
		a.ai.bar.followUp = nil
		a.ai.bar.focus = 1
	}
	a.invalidateMain()
}

// aiProposalDone 应用成功后从待审列表移除。
func (a *app) aiProposalDone(i int) {
	a.aiDismiss(i)
	if len(a.ai.bar.proposals) > 0 {
		// 追问上下文仍指向旧组；后续追问以当前待审组为准意义不大，
		// 清掉避免把已应用的建议再发给模型。
		a.ai.bar.followUp = nil
	}
}

// aiToggleEdit 进入/退出卡片原文编辑；进入时以建议的行播种草稿。
func (a *app) aiToggleEdit(i int) {
	if i < 0 || i >= len(a.ai.bar.proposals) {
		return
	}
	if a.ai.bar.editing == i {
		a.aiCommitEdit()
		return
	}
	a.ai.bar.editing = i
	p := a.ai.bar.proposals[i]
	switch p.Kind {
	case ai.KindNewTab:
		a.ai.bar.editDraft = p.NewTab.Content
	default:
		a.ai.bar.editDraft = strings.Join(p.Lines, "\n")
	}
	a.invalidateMain()
}

// aiCommitEdit 把编辑草稿写回建议（编辑原文 = 直接改要写入的 Markdown）。
func (a *app) aiCommitEdit() {
	i := a.ai.bar.editing
	if i < 0 || i >= len(a.ai.bar.proposals) {
		a.ai.bar.editing = -1
		return
	}
	p := &a.ai.bar.proposals[i]
	switch p.Kind {
	case ai.KindNewTab:
		p.NewTab.Content = a.ai.bar.editDraft
	default:
		p.Lines = strings.Split(a.ai.bar.editDraft, "\n")
	}
	a.ai.bar.editing = -1
	a.invalidateMain()
}
