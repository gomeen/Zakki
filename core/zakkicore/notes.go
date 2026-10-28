package zakkicore

import (
	"encoding/json"
	"errors"
	"fmt"

	"zakki/core/model"
)

// notesTab 是绑定边界上的标签形状；磁盘上各层的未知字段不跨边界，
// 由 model.Decode/Encode 在原文件里保留。
type notesTab struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	Color string `json:"color"`
}

// notesSnapshot 是 DecodeNotes 的返回形状；reason 取值见契约表，
// 不支持写入时不返回 tabs（null），legacy 的 v1 文件 tabs 为空数组。
type notesSnapshot struct {
	Writable bool       `json:"writable"`
	Reason   string     `json:"reason"`
	Version  int        `json:"version"`
	Active   int        `json:"active"`
	Tabs     []notesTab `json:"tabs"`
}

// DecodeNotes 解码 zakki.json 供移动端分类展示：version/active/tabs 一律按磁盘
// 原样返回，不做迁移与修正（写路径才要求当前版本）；内部异常走 recover 防线。
func DecodeNotes(raw string) (out string, err error) {
	defer guard(&out, &err)
	var disk struct {
		Version int        `json:"version"`
		Active  int        `json:"active"`
		Tabs    []notesTab `json:"tabs"`
	}
	if e := json.Unmarshal([]byte(raw), &disk); e != nil {
		return marshalNotesSnapshot(notesSnapshot{Reason: "invalid"}), nil
	}
	switch {
	case disk.Version > model.CurrentDataVersion:
		return marshalNotesSnapshot(notesSnapshot{Reason: "versionTooNew", Version: disk.Version, Active: disk.Active}), nil
	case disk.Version < model.CurrentDataVersion:
		tabs := disk.Tabs
		if tabs == nil {
			tabs = []notesTab{}
		}
		return marshalNotesSnapshot(notesSnapshot{Reason: "legacyVersion", Version: disk.Version, Active: disk.Active, Tabs: tabs}), nil
	case len(disk.Tabs) > model.MaxTabCount:
		return marshalNotesSnapshot(notesSnapshot{Reason: "tooManyTabs", Version: disk.Version, Active: disk.Active}), nil
	}
	return marshalNotesSnapshot(notesSnapshot{Writable: true, Reason: "ok", Version: disk.Version, Active: disk.Active, Tabs: disk.Tabs}), nil
}

func marshalNotesSnapshot(s notesSnapshot) string {
	b, e := json.Marshal(s)
	if e != nil {
		panic(e) // 形状固定不会发生；交给 recover 防线按内部异常处理
	}
	return string(b)
}

// EncodeNotes 把 tabsJSON（notesTab 数组）按 id 覆盖 raw 中对应标签的已知字段，
// 保留所有未知字段（顶层/settings/各 tab）与桌面专属设置原值。
// 不做迁移：raw 必须已是当前版本，否则返回 write_protected；绝不部分写出。
func EncodeNotes(raw, tabsJSON string) (out string, err error) {
	defer guard(&out, &err)
	// 写保护分类先于任何修改；分类口径与 DecodeNotes 一致。
	var peek struct {
		Version int               `json:"version"`
		Tabs    []json.RawMessage `json:"tabs"`
	}
	if e := json.Unmarshal([]byte(raw), &peek); e != nil {
		return "", coded("write_protected", errors.New("文件损坏，无法解析"))
	}
	if peek.Version > model.CurrentDataVersion {
		return "", coded("write_protected", &model.VersionError{Version: peek.Version})
	}
	if peek.Version < model.CurrentDataVersion {
		return "", coded("write_protected", errors.New("文件版本低于当前支持，请先用桌面端打开升级"))
	}
	if len(peek.Tabs) > model.MaxTabCount {
		return "", coded("write_protected", fmt.Errorf("%w：%d 个，最多 %d 个", model.ErrTooManyTabs, len(peek.Tabs), model.MaxTabCount))
	}
	var updates []notesTab
	if e := json.Unmarshal([]byte(tabsJSON), &updates); e != nil {
		return "", coded("invalid_json", e)
	}
	seen := make(map[uint64]bool, len(updates))
	for _, u := range updates {
		if seen[u.ID] {
			return "", coded("invalid_args", fmt.Errorf("标签 id %d 重复", u.ID))
		}
		seen[u.ID] = true
	}
	doc, e := model.Decode([]byte(raw), model.MigrateOptions{DefaultTabCount: model.DefaultTabCount, MaxTabCount: model.MaxTabCount})
	if e != nil {
		return "", coded("write_protected", e)
	}
	index := make(map[uint64]int, len(doc.File.Tabs))
	for i, tb := range doc.File.Tabs {
		index[tb.ID] = i
	}
	for _, u := range updates {
		i, ok := index[u.ID]
		if !ok {
			return "", coded("invalid_args", fmt.Errorf("标签 id %d 不在原文件中", u.ID))
		}
		doc.File.Tabs[i].Name, doc.File.Tabs[i].Text, doc.File.Tabs[i].Color = u.Name, u.Text, u.Color
	}
	encoded, e := model.Encode(doc)
	if e != nil {
		return "", fmt.Errorf("internal: 内部处理异常: %w", e)
	}
	return string(encoded), nil
}
