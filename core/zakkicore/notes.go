package zakkicore

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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

// ---- M2-4 标签管理写路径（桌面 parity 参照 internal/app/tab_model.go） ----

// decodeForWrite 是标签管理写路径共用的前置分类：解析失败/版本超前/legacy/
// 标签超限一律 write_protected（口径与 EncodeNotes 完全一致），通过后按三层
// 未知字段保真解码。DefaultTabCount 留零：写路径不补默认标签，极端的无标签
// 文件加标签时不会凭空长出 7 个默认标签，新 id 才能按现有最大 id+1（从 1 起）。
func decodeForWrite(raw string) (model.Document, error) {
	var peek struct {
		Version int               `json:"version"`
		Tabs    []json.RawMessage `json:"tabs"`
	}
	if e := json.Unmarshal([]byte(raw), &peek); e != nil {
		return model.Document{}, coded("write_protected", errors.New("文件损坏，无法解析"))
	}
	if peek.Version > model.CurrentDataVersion {
		return model.Document{}, coded("write_protected", &model.VersionError{Version: peek.Version})
	}
	if peek.Version < model.CurrentDataVersion {
		return model.Document{}, coded("write_protected", errors.New("文件版本低于当前支持，请先用桌面端打开升级"))
	}
	if len(peek.Tabs) > model.MaxTabCount {
		return model.Document{}, coded("write_protected", fmt.Errorf("%w：%d 个，最多 %d 个", model.ErrTooManyTabs, len(peek.Tabs), model.MaxTabCount))
	}
	doc, e := model.Decode([]byte(raw), model.MigrateOptions{MaxTabCount: model.MaxTabCount})
	if e != nil {
		return model.Document{}, coded("write_protected", e)
	}
	return doc, nil
}

func encodeForWrite(doc model.Document) (string, error) {
	encoded, e := model.Encode(doc)
	if e != nil {
		return "", fmt.Errorf("internal: 内部处理异常: %w", e)
	}
	return string(encoded), nil
}

func tabIndexByID(tabs []model.Tab, id uint64) int {
	for i, tb := range tabs {
		if tb.ID == id {
			return i
		}
	}
	return -1
}

// clampIndex 把下标收敛到 0..n-1；调用方保证 n > 0（调用点都已有标签）。
func clampIndex(v, n int) int {
	return min(max(v, 0), n-1)
}

// addNoteTabResult 是 AddNoteTab 的返回形状；新 id 由客户端从这里取，不自行计算。
type addNoteTabResult struct {
	Raw string `json:"raw"`
	ID  uint64 `json:"id"`
}

// AddNoteTab 在 raw 末尾追加一个标签，返回 {"raw":新文件JSON,"id":新id}。
// 写保护分类先行（与 EncodeNotes 同口径）；已有 MaxTabCount 个标签时报
// invalid_args——先自查 len 再解码，9 满文件超限给 invalid_args 而非
// write_protected。新 id 为现有最大 id+1（无标签文件从 1 起）；active 落到
// 末尾新索引（桌面 addTab parity）。Name/Color 原样存储，命名与选色职责在客户端。
func AddNoteTab(raw, name, color string) (out string, err error) {
	defer guard(&out, &err)
	doc, err := decodeForWrite(raw)
	if err != nil {
		return "", err
	}
	if len(doc.File.Tabs) >= model.MaxTabCount {
		return "", coded("invalid_args", fmt.Errorf("已有 %d 个标签，最多 %d 个", len(doc.File.Tabs), model.MaxTabCount))
	}
	var maxID uint64
	for _, tb := range doc.File.Tabs {
		if tb.ID > maxID {
			maxID = tb.ID
		}
	}
	id := maxID + 1
	doc.File.Tabs = append(doc.File.Tabs, model.Tab{ID: id, Name: name, Color: color})
	doc.File.Active = len(doc.File.Tabs) - 1
	encoded, e := encodeForWrite(doc)
	if e != nil {
		return "", e
	}
	res, e := json.Marshal(addNoteTabResult{Raw: encoded, ID: id})
	if e != nil {
		panic(e) // 形状固定不会发生；交给 recover 防线按内部异常处理
	}
	return string(res), nil
}

// DeleteNoteTab 按 id 删除标签并返回整个新文件 JSON。写保护先行；只剩一个
// 标签（桌面「至少保留一个」）与 id 不存在都报 invalid_args。active 语义与
// 桌面 deleteTabNow 一致：删的是 active 落到 min(i, len-1)，否则按 id 跟随
// 原 active 标签，末尾 clamp。
func DeleteNoteTab(raw string, id int64) (out string, err error) {
	defer guard(&out, &err)
	if id < 0 {
		return "", coded("invalid_args", fmt.Errorf("标签 id %d 非法", id))
	}
	doc, err := decodeForWrite(raw)
	if err != nil {
		return "", err
	}
	i := tabIndexByID(doc.File.Tabs, uint64(id))
	if i < 0 {
		return "", coded("invalid_args", fmt.Errorf("标签 id %d 不在原文件中", id))
	}
	if len(doc.File.Tabs) <= 1 {
		return "", coded("invalid_args", errors.New("至少保留一个标签"))
	}
	activeID := doc.File.Tabs[doc.File.Active].ID
	doc.File.Tabs = slices.Delete(doc.File.Tabs, i, i+1)
	if doc.File.Active == i {
		doc.File.Active = min(i, len(doc.File.Tabs)-1)
	} else if k := tabIndexByID(doc.File.Tabs, activeID); k >= 0 {
		doc.File.Active = k
	}
	doc.File.Active = clampIndex(doc.File.Active, len(doc.File.Tabs))
	return encodeForWrite(doc)
}

// ReorderNoteTabs 按 idsJSON 给出的完整排列重排标签并返回整个新文件 JSON。
// 写保护先行；idsJSON 必须是现有 id 的完整排列（长度一致、无重复且集合一致），
// 否则报 invalid_args（解析失败报 invalid_json）。active 按 id 跟随到新位置
// （桌面 reorderTab parity）。
func ReorderNoteTabs(raw, idsJSON string) (out string, err error) {
	defer guard(&out, &err)
	doc, err := decodeForWrite(raw)
	if err != nil {
		return "", err
	}
	var ids []int64
	if e := json.Unmarshal([]byte(idsJSON), &ids); e != nil {
		return "", coded("invalid_json", e)
	}
	if len(ids) != len(doc.File.Tabs) {
		return "", coded("invalid_args", fmt.Errorf("排列含 %d 个 id，与标签数 %d 不一致", len(ids), len(doc.File.Tabs)))
	}
	byID := make(map[uint64]model.Tab, len(doc.File.Tabs))
	for _, tb := range doc.File.Tabs {
		byID[tb.ID] = tb
	}
	reordered := make([]model.Tab, 0, len(ids))
	seen := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		if id < 0 {
			return "", coded("invalid_args", fmt.Errorf("标签 id %d 非法", id))
		}
		tb, ok := byID[uint64(id)]
		if !ok {
			return "", coded("invalid_args", fmt.Errorf("标签 id %d 不在原文件中", id))
		}
		if seen[uint64(id)] {
			return "", coded("invalid_args", fmt.Errorf("标签 id %d 重复", id))
		}
		seen[uint64(id)] = true
		reordered = append(reordered, tb)
	}
	// activeID 必须在重排前捕获（桌面 reorderTab 同样先拿原 active 标签的 id）。
	var activeID uint64
	if len(doc.File.Tabs) > 0 {
		activeID = doc.File.Tabs[doc.File.Active].ID
	}
	doc.File.Tabs = reordered
	if len(doc.File.Tabs) > 0 {
		if k := tabIndexByID(doc.File.Tabs, activeID); k >= 0 {
			doc.File.Active = k
		}
		doc.File.Active = clampIndex(doc.File.Active, len(doc.File.Tabs))
	}
	return encodeForWrite(doc)
}
