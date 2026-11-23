package ai

// userPrompt 的多标签上下文构造：其他标签只读、逐个截断、总量封顶。

import (
	"strings"
	"testing"
	"time"
)

func TestUserPromptOtherTabs(t *testing.T) {
	req := AskRequest{
		Instruction: "总结一下", TabName: "待办", NoteMarkdown: "- [ ] 买牛奶",
		Others: []TabContext{
			{Name: "灵感", Markdown: "想法 A"},
			{Name: "", Markdown: "匿名内容"},
		},
		Now: time.Date(2026, 11, 23, 12, 0, 0, 0, time.Local),
	}
	up := userPrompt(req)
	for _, want := range []string{
		"Current tab: 待办", "- [ ] 买牛奶",
		"Other tabs (read-only context", "## Tab 「灵感」", "想法 A", "## Tab 「(untitled)」", "匿名内容",
		"Instruction: 总结一下",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("userPrompt 缺少 %q\n---\n%s", want, up)
		}
	}
}

func TestUserPromptOtherTabsBudget(t *testing.T) {
	big := strings.Repeat("长", maxOtherTabRunes*2) // 单标签超限
	many := make([]TabContext, 0, 16)
	for i := 0; i < 16; i++ { // 总量超限
		many = append(many, TabContext{Name: "签", Markdown: strings.Repeat("字", 2000)})
	}
	req := AskRequest{Instruction: "x", TabName: "当前", NoteMarkdown: "n",
		Others: append([]TabContext{{Name: "大", Markdown: big}}, many...),
		Now:    time.Date(2026, 11, 23, 12, 0, 0, 0, time.Local),
	}
	up := userPrompt(req)
	if got := strings.Count(up, "长"); got >= maxOtherTabRunes*2 {
		t.Errorf("超大标签应被截断，出现 %d 次", got)
	}
	if got := strings.Count(up, "字"); got > maxOthersRunes+maxOtherTabRunes+4000 {
		t.Errorf("其他标签总量超预算：出现 %d 次", got)
	}
	// 系统提示里说明编辑只锚定当前标签
	if !strings.Contains(systemPrompt(time.Now()), "CURRENT tab only") {
		t.Error("systemPrompt 应说明编辑建议只针对当前标签")
	}
}
