package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
)

// Document 是一次 Decode 的完整结果：迁移后的 File 与三层未知字段。
// 移动端只改已知字段后经 Encode 写回，未知字段原样保留；
// 桌面后续切换到这套读写（当前写盘仍是无未知字段保留的 MarshalIndent）。
type Document struct {
	// File 是走完 Migrate 的已知字段。
	File File
	// Unknown 是顶层的未知字段。
	Unknown map[string]json.RawMessage
	// SettingsUnknown 是 settings 节的未知字段。
	SettingsUnknown map[string]json.RawMessage
	// TabUnknown 是各 tab 的未知字段，按解码时磁盘上的 tab id 索引。
	// Migrate 只在 ID 非法（重复/缺号）的异常文件里重排 ID，此时无法对应的
	// 条目在 Encode 时丢弃；正常文件 ID 原样保留，不存在这个问题。
	TabUnknown map[uint64]map[string]json.RawMessage
}

// 已知键集合由结构体 json 标签推导，与 Encode 的字段顺序共用一份来源；
// 结构体是用户契约，改键名必须走数据版本迁移。
var (
	fileKnownKeys     = jsonKnownKeys(File{})
	settingsKnownKeys = jsonKnownKeys(Settings{})
	tabKnownKeys      = jsonKnownKeys(Tab{})
)

// Decode 解码 raw 并迁移到当前版本：先按未知字段友好地展开三层结构，
// 再走 Migrate（版本超前 / 标签超限以 Migrate 的错误返回）。
// 与桌面载入一致：settings.hideOnEscape 缺键按 true（JSON bool 分不清缺键与显式 false）。
func Decode(raw []byte, opt MigrateOptions) (Document, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Document{}, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return Document{}, err
	}
	doc := Document{Unknown: map[string]json.RawMessage{}, TabUnknown: map[uint64]map[string]json.RawMessage{}}
	for k, v := range top {
		if fileKnownKeys[k] {
			continue
		}
		doc.Unknown[k] = v
	}
	if settingsRaw, ok := top["settings"]; ok {
		unknown, err := splitUnknown(settingsRaw, &f.Settings, settingsKnownKeys)
		if err != nil {
			return Document{}, err
		}
		doc.SettingsUnknown = unknown
	}
	if tabsRaw, ok := top["tabs"]; ok {
		var tabNodes []json.RawMessage
		if err := json.Unmarshal(tabsRaw, &tabNodes); err != nil {
			return Document{}, err
		}
		for _, node := range tabNodes {
			var tb Tab
			unknown, err := splitUnknown(node, &tb, tabKnownKeys)
			if err != nil {
				return Document{}, err
			}
			if len(unknown) > 0 {
				doc.TabUnknown[tb.ID] = unknown
			}
		}
	}
	// 缺键按 true：settings 整体缺失与 hideOnEscape 键缺失等价。
	if _, ok := top["settings"]; !ok {
		f.Settings.HideOnEscape = true
	} else {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(top["settings"], &probe); err != nil {
			return Document{}, err
		}
		if _, ok := probe["hideOnEscape"]; !ok {
			f.Settings.HideOnEscape = true
		}
	}
	f, err := Migrate(f, opt)
	if err != nil {
		return Document{}, err
	}
	doc.File = f
	return doc, nil
}

// Encode 把 Document 编码为与桌面写盘同形的 JSON：已知键按结构体字段顺序，
// 未知键排在每层已知键之后按字典序；缩进两个空格，omitempty 空值照常省略。
func Encode(doc Document) ([]byte, error) {
	entries, err := structEntries(doc.File)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONKey(&b, e.key)
		b.WriteByte(':')
		switch e.field {
		case "Tabs":
			if err := encodeTabs(&b, doc); err != nil {
				return nil, err
			}
		case "Settings":
			if err := encodeNode(&b, doc.File.Settings, doc.SettingsUnknown); err != nil {
				return nil, err
			}
		default:
			b.Write(e.val)
		}
	}
	if err := writeUnknown(&b, doc.Unknown); err != nil {
		return nil, err
	}
	b.WriteByte('}')
	return reindent(b.Bytes())
}

func encodeTabs(b *bytes.Buffer, doc Document) error {
	b.WriteByte('[')
	for i, tb := range doc.File.Tabs {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := encodeNode(b, tb, doc.TabUnknown[tb.ID]); err != nil {
			return err
		}
	}
	b.WriteByte(']')
	return nil
}

// encodeNode 写出单个对象：已知键按字段顺序在前，未知键按字典序在后。
// 先组装压缩形式，整体由 Encode 末尾统一缩进，保证与 encoding/json 同形。
func encodeNode(b *bytes.Buffer, v any, unknown map[string]json.RawMessage) error {
	entries, err := structEntries(v)
	if err != nil {
		return err
	}
	b.WriteByte('{')
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONKey(b, e.key)
		b.WriteByte(':')
		b.Write(e.val)
	}
	if err := writeUnknown(b, unknown); err != nil {
		return err
	}
	b.WriteByte('}')
	return nil
}

func writeUnknown(b *bytes.Buffer, unknown map[string]json.RawMessage) error {
	if len(unknown) == 0 {
		return nil
	}
	keys := make([]string, 0, len(unknown))
	for k := range unknown {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		var compact bytes.Buffer
		if err := json.Compact(&compact, unknown[k]); err != nil {
			return err
		}
		b.WriteByte(',')
		writeJSONKey(b, k)
		b.WriteByte(':')
		b.Write(compact.Bytes())
	}
	return nil
}

func reindent(compact []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeJSONKey(b *bytes.Buffer, key string) {
	kj, _ := json.Marshal(key)
	b.Write(kj)
}

type structEntry struct {
	field string
	key   string
	val   []byte
}

// structEntries 按字段顺序展开结构体的已知键；omitempty 语义与写盘路径一致
// （false、0、空串、空切片/映射省略）。
func structEntries(v any) ([]structEntry, error) {
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	out := make([]structEntry, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name, opts, ok := jsonField(f)
		if !ok {
			continue
		}
		fv := rv.Field(i)
		if strings.Contains(opts, "omitempty") && isEmptyValue(fv) {
			continue
		}
		val, err := json.Marshal(fv.Interface())
		if err != nil {
			return nil, err
		}
		out = append(out, structEntry{field: f.Name, key: name, val: val})
	}
	return out, nil
}

func jsonKnownKeys(v any) map[string]bool {
	t := reflect.TypeOf(v)
	keys := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if name, _, ok := jsonField(t.Field(i)); ok {
			keys[name] = true
		}
	}
	return keys
}

func jsonField(f reflect.StructField) (name, opts string, ok bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return "", "", false
	}
	name, opts, _ = strings.Cut(tag, ",")
	if name == "-" {
		return "", "", false
	}
	return name, opts, true
}

// isEmptyValue 与 encoding/json 的 omitempty 判定一致。
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

// splitUnknown 解码 node 的已知字段并返回未知字段 map。
func splitUnknown(node []byte, into any, known map[string]bool) (map[string]json.RawMessage, error) {
	if err := json.Unmarshal(node, into); err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(node, &obj); err != nil {
		return nil, err
	}
	var unknown map[string]json.RawMessage
	for k, v := range obj {
		if known[k] {
			continue
		}
		if unknown == nil {
			unknown = map[string]json.RawMessage{}
		}
		unknown[k] = v
	}
	return unknown, nil
}
