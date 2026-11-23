package ai

// 提示词组装与提供方预设。预设表是偏好的“提供方”枚举（prefs 包里
// 重复一份枚举做校验，两处以 ID 为契约保持同步）与设置页预填的
// 唯一事实源；BaseURL 一律含版本路径、末尾不带斜杠。

import (
	"fmt"
	"strings"
	"time"
)

// Provider 是一个提供方预设。
type Provider struct {
	ID string
	// BaseURL 预填的接口地址；custom 为空串，由用户填写。
	BaseURL string
	// NeedsKey 该提供方通常需要 API 密钥（Ollama 本地服务不需要）。
	NeedsKey bool
}

// Providers 全部预设；顺序即设置页下拉顺序，custom 永远最后。
var Providers = []Provider{
	{ID: "openai", BaseURL: "https://api.openai.com/v1", NeedsKey: true},
	{ID: "deepseek", BaseURL: "https://api.deepseek.com/v1", NeedsKey: true},
	{ID: "openrouter", BaseURL: "https://openrouter.ai/api/v1", NeedsKey: true},
	{ID: "ollama", BaseURL: "http://localhost:11434/v1", NeedsKey: false},
	{ID: "custom", BaseURL: "", NeedsKey: true},
}

// ProviderByID 按ID查预设。
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// maxNoteRunes 发送给模型的当前标签上限；超出截中间（保留首尾）。
const maxNoteRunes = 12000

// 其他标签上下文的截断参数：单个上限 + 合计预算，问答够用且不撑爆请求。
const (
	maxOtherTabRunes = 3000
	maxOthersRunes   = 18000
)

// systemPrompt 是不变的系统提示：Zakki 的 Markdown 子集、时间令牌语法
// 与输出 JSON 契约。now 用于把「明天下午三点」这类相对时间锚定到具体日期。
func systemPrompt(now time.Time) string {
	var b strings.Builder
	b.WriteString("You are the writing assistant built into Zakki, a minimal markdown sticky-notes app. ")
	b.WriteString("The user types one instruction; you answer with proposed edits to their note. ")
	b.WriteString("You never chat and always answer with a single JSON object.\n\n")
	b.WriteString("The note supports this markdown subset: ATX headings (#..######), bullet lists (- ), task lists (- [ ] / - [x]), blockquotes (>), fenced code blocks, tables, and inline styles. ")
	b.WriteString("Reminders are plain tokens at the end of a task line: `@YYYY-MM-DD HH:mm` (fixed-width datetime), `~10m | ~1h | ~1d` (alert lead time), `^rule` (recurrence, e.g. ^w@1-5). ")
	b.WriteString("Reminders fire through these tokens: to add one, write a task line carrying them, e.g. `- Standup @2026-03-02 09:30 ~10m`.\n\n")
	b.WriteString("Rules:\n")
	fmt.Fprintf(&b, "- Today is %s (%s). Resolve relative dates and times to exact tokens.\n", now.Format("2006-01-02 15:04"), now.Weekday())
	b.WriteString("- Write summary and note content in the same language as the user's instruction.\n")
	b.WriteString("- Propose the smallest set of edits that satisfies the instruction; at most 4 proposals.\n")
	b.WriteString("- Add/change requests are action requests: always propose the closest useful edit, even if a similar line already exists (add a lead time, a new entry, or a duplicate). Leave proposals empty only when the instruction is clearly a pure question or chat.\n")
	b.WriteString("- Edit proposals must target the CURRENT tab only; other tabs listed below are read-only context for answering questions, never anchor targets.\n")
	b.WriteString("- For kind \"replace\", anchor MUST be copied VERBATIM from the note (an exact, unique substring); lines replace the anchor's whole lines. Empty lines means deleting them.\n")
	b.WriteString("- For kind \"insert\", at is one of \"docEnd\" (end of note), \"listEnd\" (after the last list item), \"afterAnchor\" (right after the anchor's line; requires anchor).\n")
	b.WriteString("- For kind \"newTab\", propose a new tab (name + full markdown content); never touch the current note in the same proposal.\n")
	b.WriteString("- The note may be elided in the middle (…); never anchor on elided text.\n\n")
	b.WriteString("Answer with exactly one JSON object:\n")
	b.WriteString(`{"answer":"1-2 sentences in the user's language","proposals":[{"kind":"replace|insert|newTab","summary":"short verb phrase","anchor":"verbatim note text (replace/afterAnchor only)","at":"docEnd|listEnd|afterAnchor","lines":["- [ ] task"],"tab":"new tab name (newTab only)","content":"markdown (newTab only)"}]}` + "\n")
	b.WriteString("- \"answer\" is REQUIRED, never empty: for an edit request, state in one sentence what you propose; for a question or chat, answer it directly from the notes (all tabs are provided).\n")
	return b.String()
}

// userPrompt 组装用户消息：标签名、（可能截断的）笔记、指令与追问上下文。
func userPrompt(req AskRequest) string {
	var b strings.Builder
	name := strings.TrimSpace(req.TabName)
	if name == "" {
		name = "(untitled)"
	}
	fmt.Fprintf(&b, "Current tab: %s\n\n", name)
	b.WriteString("Note (may be elided in the middle):\n")
	b.WriteString(truncateMiddle(req.NoteMarkdown, maxNoteRunes))
	b.WriteString("\n\n")
	// 其他标签：只读上下文，供问答引用；逐个截断并控制总量。
	if len(req.Others) > 0 {
		b.WriteString("Other tabs (read-only context; edit proposals never target these):\n")
		budget := maxOthersRunes
		for _, tb := range req.Others {
			if budget <= 0 {
				break
			}
			nm := strings.TrimSpace(tb.Name)
			if nm == "" {
				nm = "(untitled)"
			}
			fmt.Fprintf(&b, "## Tab 「%s」\n", nm)
			shown := truncateMiddle(tb.Markdown, min(maxOtherTabRunes, budget))
			b.WriteString(shown)
			b.WriteString("\n\n")
			budget -= len([]rune(shown))
		}
	}
	if req.FollowUp != nil {
		fmt.Fprintf(&b, "Previous instruction: %s\n", req.FollowUp.Instruction)
		b.WriteString("Your previous proposals (now superseded):\n")
		b.WriteString(previousJSON(req.FollowUp.Result))
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "Adjustment: %s\n", req.Instruction)
		return b.String() + "Reply with the FULL replacement set of proposals following the same JSON contract."
	}
	fmt.Fprintf(&b, "Instruction: %s\n", req.Instruction)
	b.WriteString("Reply with the JSON object only.")
	return b.String()
}

// previousJSON 把上一轮建议序列化回模型契约的 JSON（仅用于追问上下文）。
func previousJSON(res AskResult) string {
	var b strings.Builder
	b.WriteString(`{"proposals":[`)
	for i, p := range res.Proposals {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"kind":%q,"summary":%q,"anchor":%q,"at":%q,"lines":%q,"tab":%q,"content":%q}`,
			string(p.Kind), p.Summary, p.Anchor, p.At, strings.Join(p.Lines, "\n"), p.NewTab.Name, p.NewTab.Content)
	}
	b.WriteString(`]}`)
	return b.String()
}

// truncateMiddle 超过 max rune 时保留首尾各半、中间插省略标记。
func truncateMiddle(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	half := max / 2
	elided := len(r) - max
	return string(r[:half]) + fmt.Sprintf("\n…（中间省略 %d 字）…\n", elided) + string(r[len(r)-half:])
}
