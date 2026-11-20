package ai

// 建议的数据模型与校验。所有动作都归约为三种：在当前笔记插入行、
// 替换锚点行、新建标签写入内容。提醒不需要专门动作——带时间令牌的
// 任务行就是提醒，现有调度器自动接手。

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Kind 建议的动作类型。
type Kind string

const (
	// KindInsert 在当前笔记插入行。
	KindInsert Kind = "insert"
	// KindReplace 替换锚点所在的整行。
	KindReplace Kind = "replace"
	// KindNewTab 新建标签并写入内容。
	KindNewTab Kind = "newTab"
)

// 插入位置（At）的合法取值。
const (
	// AtDocEnd 文档末尾。
	AtDocEnd = "docEnd"
	// AtListEnd 最后一个列表项之后；没有列表时退化为文档末尾。
	AtListEnd = "listEnd"
	// AtAfterAnchor 锚点所在行的下一行；必须同时给 Anchor。
	AtAfterAnchor = "afterAnchor"
)

// TabSpec 是 newTab 建议的目标标签。
type TabSpec struct {
	Name    string
	Content string
}

// Proposal 是一条待用户确认的建议。
type Proposal struct {
	Kind    Kind
	Summary string // 模型给的动词短语（跟随指令语言），如「整理成待办」
	Anchor  string // replace / afterAnchor：笔记原文的精确片段
	At      string // insert 位置
	Lines   []string
	NewTab  TabSpec
}

// AskRequest 是一次提问的输入。
type AskRequest struct {
	Instruction  string
	TabName      string
	NoteMarkdown string
	// Now 提问时刻：用于把相对日期锚定到具体日期；零值时取 time.Now。
	Now time.Time
	// FollowUp 非空表示追问：附上一轮指令与建议，要求返回整组替换。
	FollowUp *FollowUp
}

// FollowUp 是追问的上下文：上一轮指令与它的建议。
type FollowUp struct {
	Instruction string
	Result      AskResult
}

// AskResult 是一次提问的输出。
type AskResult struct {
	Proposals []Proposal
}

// proposalWire 是模型 JSON 的字段名。
type proposalWire struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Anchor  string   `json:"anchor"`
	At      string   `json:"at"`
	Lines   []string `json:"lines"`
	Tab     string   `json:"tab"`
	Content string   `json:"content"`
}

// parseProposals 从模型输出提取并解析建议组：容忍 ```json 围栏与前后
// 混入的说明文字（取首个 { 到最后一个 }），无法解析时如实报错。
func parseProposals(content string) (AskResult, error) {
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return AskResult{}, fmt.Errorf("%w: 输出里没有 JSON 对象", ErrParse)
	}
	var wire struct {
		Proposals []proposalWire `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &wire); err != nil {
		return AskResult{}, fmt.Errorf("%w: %v", ErrParse, err)
	}
	res := AskResult{}
	for _, p := range wire.Proposals {
		kind := Kind(strings.TrimSpace(p.Kind))
		switch kind {
		case KindInsert, KindReplace, KindNewTab:
		default:
			continue // 未知类型跳过，不整体失败
		}
		res.Proposals = append(res.Proposals, Proposal{
			Kind:    kind,
			Summary: strings.TrimSpace(p.Summary),
			Anchor:  p.Anchor,
			At:      strings.TrimSpace(p.At),
			Lines:   p.Lines,
			NewTab:  TabSpec{Name: strings.TrimSpace(p.Tab), Content: p.Content},
		})
	}
	if len(res.Proposals) == 0 {
		return AskResult{}, fmt.Errorf("%w: 没有可用的建议", ErrParse)
	}
	return res, nil
}

// Validate 校验建议组与笔记的一致性：replace / afterAnchor 的锚点必须
// 在笔记中恰好出现一次；insert 的位置必须合法；newTab 必须有名字。
func (r AskResult) Validate(note string) error {
	for _, p := range r.Proposals {
		switch p.Kind {
		case KindInsert:
			switch p.At {
			case AtDocEnd, AtListEnd:
			case AtAfterAnchor:
				if err := validateAnchor(p.Anchor, note); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%w: 未知插入位置 %q", ErrParse, p.At)
			}
			if len(p.Lines) == 0 {
				return fmt.Errorf("%w: 插入建议没有内容", ErrParse)
			}
		case KindReplace:
			if err := validateAnchor(p.Anchor, note); err != nil {
				return err
			}
		case KindNewTab:
			if p.NewTab.Name == "" {
				return fmt.Errorf("%w: 新建标签建议缺少名字", ErrParse)
			}
		}
	}
	return nil
}

// validateAnchor 要求锚点非空、非纯空白，且在笔记中恰好出现一次。
func validateAnchor(anchor, note string) error {
	if strings.TrimSpace(anchor) == "" {
		return fmt.Errorf("%w: 建议缺少锚点", ErrAnchorNotFound)
	}
	switch strings.Count(note, anchor) {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("%w", ErrAnchorNotFound)
	default:
		return fmt.Errorf("%w: 锚点在笔记中出现多次", ErrAnchorNotFound)
	}
}
