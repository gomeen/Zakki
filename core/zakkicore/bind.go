// Package zakkicore 提供 gomobile 可绑定的 JSON 边界。
package zakkicore

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"zakki/core"
)

type suffix struct {
	Due      string `json:"due"`
	DueMs    int64  `json:"dueMs"`
	AlertMs  int64  `json:"alertMs"`
	HasAlert bool   `json:"hasAlert"`
	Recur    string `json:"recur"`
}

func guard[T any](out *T, err *error) {
	if recover() != nil {
		var zero T
		*out = zero
		*err = fmt.Errorf("internal: 内部处理异常")
	}
}
func coded(code string, err error) error {
	description := map[string]string{
		"invalid_json":    "JSON 格式非法",
		"invalid_due":     "截止时刻非法",
		"invalid_alert":   "提醒配置非法",
		"invalid_recur":   "重复后缀非法",
		"invalid_rule":    "重复规则非法",
		"invalid_args":    "参数非法",
		"invalid_offset":  "UTF-16 保护区间非法",
		"write_protected": "原文件处于写保护状态",
	}[code]
	return fmt.Errorf("%s: %s: %w", code, description, err)
}
func suffixJSON(t *core.TimeSuffix) *suffix {
	if t == nil {
		return nil
	}
	return &suffix{Due: t.Time.Format("2006-01-02 15:04"), DueMs: t.Time.UnixMilli(), AlertMs: int64(t.Alert / time.Millisecond), HasAlert: t.HasAlert, Recur: t.Recur}
}
func decodeSuffix(raw string) (*core.TimeSuffix, error) {
	if raw == "" || strings.TrimSpace(raw) == "null" {
		return nil, nil
	}
	var v struct {
		Due      string `json:"due"`
		AlertMs  int64  `json:"alertMs"`
		HasAlert bool   `json:"hasAlert"`
		Recur    string `json:"recur"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, coded("invalid_json", err)
	}
	if v.Due == "" {
		return nil, coded("invalid_due", fmt.Errorf("due 为空"))
	}
	loc := core.Location()
	due, err := time.ParseInLocation("2006-01-02 15:04", v.Due, loc)
	if err != nil || due.Format("2006-01-02 15:04") != v.Due {
		return nil, coded("invalid_due", fmt.Errorf("时刻非法"))
	}
	if !v.HasAlert && v.AlertMs != 0 {
		return nil, coded("invalid_alert", fmt.Errorf("未启用提醒却有提前量"))
	}
	valid := !v.HasAlert
	for _, tier := range core.AlertTiers() {
		if v.HasAlert && v.AlertMs == int64(tier.Span/time.Millisecond) {
			valid = true
		}
	}
	if !valid {
		return nil, coded("invalid_alert", fmt.Errorf("提醒档位非法"))
	}
	if v.Recur != "" {
		if _, err := core.ParseRule(v.Recur); err != nil {
			return nil, coded("invalid_recur", err)
		}
	}
	return &core.TimeSuffix{Time: due, Alert: time.Duration(v.AlertMs) * time.Millisecond, HasAlert: v.HasAlert, Recur: v.Recur}, nil
}
func protected(line, raw string) ([][2]int, error) {
	if raw == "" || strings.TrimSpace(raw) == "null" {
		return nil, nil
	}
	var spans [][]*int
	if err := json.Unmarshal([]byte(raw), &spans); err != nil {
		return nil, coded("invalid_json", err)
	}
	total := len(utf16.Encode([]rune(line)))
	offsets := map[int]int{0: 0}
	n := 0
	for i, r := range line {
		offsets[n] = i
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	offsets[n] = len(line)
	out := make([][2]int, 0, len(spans))
	for _, s := range spans {
		if len(s) != 2 || s[0] == nil || s[1] == nil {
			return nil, coded("invalid_offset", fmt.Errorf("每项必须恰好包含两个整数偏移"))
		}
		start, end := *s[0], *s[1]
		if start < 0 || end < start || end > total {
			return nil, coded("invalid_offset", fmt.Errorf("区间越界"))
		}
		a, okA := offsets[start]
		b, okB := offsets[end]
		if !okA || !okB {
			return nil, coded("invalid_offset", fmt.Errorf("区间割裂代理项"))
		}
		out = append(out, [2]int{a, b})
	}
	return out, nil
}
func utf16Offset(s string) int { return len(utf16.Encode([]rune(s))) }

// FindTimeToken 返回第一个未受保护令牌及 UTF-16 半开偏移。
func FindTimeToken(line, protectedJSON string) (out string, err error) {
	defer guard(&out, &err)
	spans, e := protected(line, protectedJSON)
	if e != nil {
		return "", e
	}
	t, ok := core.FindTimeTokenEx(line, spans)
	v := struct {
		Found  bool    `json:"found"`
		Start  int     `json:"start"`
		End    int     `json:"end"`
		Suffix *suffix `json:"suffix"`
	}{Found: ok}
	if ok {
		v.Start = utf16Offset(line[:t.Start])
		v.End = utf16Offset(line[:t.End])
		v.Suffix = suffixJSON(&t.Value)
	}
	b, _ := json.Marshal(v)
	return string(b), nil
}

// ReplaceTimeToken 原位替换令牌，空串或 null 清除。
func ReplaceTimeToken(line, suffixJSON, protectedJSON string) (out string, err error) {
	defer guard(&out, &err)
	v, e := decodeSuffix(suffixJSON)
	if e != nil {
		return "", e
	}
	spans, e := protected(line, protectedJSON)
	if e != nil {
		return "", e
	}
	return core.ReplaceTimeTokenEx(line, v, spans), nil
}

// SplitTimeSuffix 返回正文与合法行尾令牌。
func SplitTimeSuffix(line string) (out string, err error) {
	defer guard(&out, &err)
	text, v := core.SplitTimeSuffix(line)
	b, _ := json.Marshal(struct {
		Text   string  `json:"text"`
		Suffix *suffix `json:"suffix"`
	}{text, suffixJSON(v)})
	return string(b), nil
}

// JoinTimeSuffix 按规范顺序写回令牌。
func JoinTimeSuffix(text, suffixJSON string) (out string, err error) {
	defer guard(&out, &err)
	v, e := decodeSuffix(suffixJSON)
	if e != nil {
		return "", e
	}
	return core.JoinTimeSuffix(text, v), nil
}

// ParseRule 校验规则并返回 JSON。
func ParseRule(s string) (out string, err error) {
	defer guard(&out, &err)
	out, err = core.ParseRule(s)
	if err != nil {
		err = coded("invalid_rule", err)
	}
	return
}

// Expand 返回未来实例数组。
func Expand(ruleJSON string, anchorMs, nowMs int64, horizonDays, maxCount int) (out string, err error) {
	defer guard(&out, &err)
	if e := validateRuleJSON(ruleJSON); e != nil {
		return "", e
	}
	if horizonDays > 3660 || maxCount > 1000 {
		return "", coded("invalid_args", fmt.Errorf("地平线或数量超限"))
	}
	out, err = core.Expand(ruleJSON, anchorMs, nowMs, horizonDays, maxCount)
	if err != nil {
		err = coded("invalid_rule", err)
	}
	return
}

// Occurrence 返回当前实例。
func Occurrence(ruleJSON string, anchorMs, nowMs int64) (out string, err error) {
	defer guard(&out, &err)
	if e := validateRuleJSON(ruleJSON); e != nil {
		return "", e
	}
	out, err = core.Occurrence(ruleJSON, anchorMs, nowMs)
	if err != nil {
		err = coded("invalid_rule", err)
	}
	return
}

// Advance 返回下一实例。
func Advance(ruleJSON string, anchorMs, nowMs int64) (out string, err error) {
	defer guard(&out, &err)
	if e := validateRuleJSON(ruleJSON); e != nil {
		return "", e
	}
	out, err = core.Advance(ruleJSON, anchorMs, nowMs)
	if err != nil {
		err = coded("invalid_rule", err)
	}
	return
}

// AlertTiers 返回提醒档位 JSON。
func AlertTiers() (out string, err error) {
	defer guard(&out, &err)
	tiers := core.AlertTiers()
	v := make([]struct {
		Suffix string `json:"suffix"`
		Ms     int64  `json:"ms"`
	}, len(tiers))
	for i, t := range tiers {
		v[i].Suffix = t.Suffix
		v[i].Ms = int64(t.Span / time.Millisecond)
	}
	b, _ := json.Marshal(v)
	return string(b), nil
}

// SetLocation 原子切换时区，空串恢复 time.Local。
func SetLocation(name string) (out string, err error) {
	defer guard(&out, &err)
	if e := core.SetLocation(strings.TrimSpace(name)); e != nil {
		return "", coded("invalid_args", e)
	}
	return "", nil
}

// RuleSyntaxVersion 是移动端可读取的重复规则语法版本。
func RuleSyntaxVersion() (version int, err error) {
	defer guard(&version, &err)
	return core.RuleSyntaxVersion, nil
}

func validateRuleJSON(raw string) error {
	var rule core.Rule
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		return coded("invalid_json", err)
	}
	return nil
}
