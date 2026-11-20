package app

// 命令注册表：菜单、Keymap 设置页与编辑器共用同一份定义。
//
//   - 每个命令有稳定 ID、显示名、默认绑定、上下文条件与执行入口。
//   - Keymap 只开放 Markdown 格式命令与全局唤起；搜索、复制、粘贴、撤销
//     等保持系统惯例，切点等既有操作保持固定绑定，不在 Keymap 开放改键。
//   - 绑定保存为规范化 accelerator 字符串（MyGo 格式），macOS 界面显示符号。
//   - 第一版仅支持单组组合键，不支持多段 chord。

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

// ---- 命令 ID ----

const (
	cmdBold           = "format.bold"
	cmdItalic         = "format.italic"
	cmdInlineCode     = "format.code"
	cmdLink           = "format.link"
	cmdH1             = "heading.1"
	cmdH2             = "heading.2"
	cmdH3             = "heading.3"
	cmdH4             = "heading.4"
	cmdH5             = "heading.5"
	cmdH6             = "heading.6"
	cmdBody           = "heading.body"
	cmdQuote          = "block.quote"
	cmdBullet         = "list.bullet"
	cmdOrdered        = "list.ordered"
	cmdTask           = "list.task"
	cmdTaskTime       = "list.taskTime"
	cmdCodeBlock      = "block.code"
	cmdRule           = "insert.rule"
	cmdTable          = "insert.table"
	cmdToggleWindow   = "global.toggleWindow"
	cmdAssistant      = "global.assistant"
	cmdAssistantFocus = "global.assistantFocus"
)

// ---- 绑定（规范化组合键） ----

// keyBinding 是一组规范化的单组组合键（不支持 chord）。primary 标记
// accelerator 使用逻辑 CmdOrCtrl；mods 始终保存目标平台解析后的物理修饰键。
type keyBinding struct {
	mods    ui.Modifiers
	key     ui.Key
	primary bool
}

func primaryModifier(goos string) ui.Modifiers {
	if goos == "darwin" {
		return ui.Super
	}
	return ui.Ctrl
}

// keyName 是 ui.Key 与 accelerator 名称的互查表（与 MyGo 的 keyName 一致）。
var namedKeyByName = map[string]ui.Key{}
var keyNameByKey = map[ui.Key]string{}

func init() {
	byName := map[string]ui.Key{
		"enter": ui.KeyEnter, "return": ui.KeyEnter, "escape": ui.KeyEscape, "esc": ui.KeyEscape,
		"backspace": ui.KeyBackspace, "tab": ui.KeyTab, "space": ui.KeySpace,
		"delete": ui.KeyDelete, "del": ui.KeyDelete, "insert": ui.KeyInsert,
		"home": ui.KeyHome, "end": ui.KeyEnd, "pageup": ui.KeyPageUp, "pagedown": ui.KeyPageDown,
		"left": ui.KeyLeft, "right": ui.KeyRight, "up": ui.KeyUp, "down": ui.KeyDown,
		"minus": ui.KeyMinus, "equal": ui.KeyEqual, "comma": ui.KeyComma, "period": ui.KeyPeriod,
		"slash": ui.KeySlash, "semicolon": ui.KeySemicolon, "quote": ui.KeyQuote,
		"bracketleft": ui.KeyBracketLeft, "bracketright": ui.KeyBracketRight,
		"backslash": ui.KeyBackslash, "backquote": ui.KeyBackquote,
	}
	for name, key := range byName {
		namedKeyByName[name] = key
	}
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		keyNameByKey[k] = string(rune('a' + int(k-ui.KeyA)))
		namedKeyByName[keyNameByKey[k]] = k
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		keyNameByKey[k] = string(rune('0' + int(k-ui.Key0)))
		namedKeyByName[keyNameByKey[k]] = k
	}
	for k := ui.KeyF1; k <= ui.KeyF12; k++ {
		name := fmt.Sprintf("f%d", int(k-ui.KeyF1)+1)
		keyNameByKey[k] = strings.ToUpper(name)
		namedKeyByName[name] = k
	}
	for k, name := range map[ui.Key]string{
		ui.KeyEnter: "Enter", ui.KeyEscape: "Escape", ui.KeyBackspace: "Backspace",
		ui.KeyTab: "Tab", ui.KeySpace: "Space", ui.KeyDelete: "Delete", ui.KeyInsert: "Insert",
		ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
		ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
		ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".",
		ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'",
		ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
	} {
		keyNameByKey[k] = name
		namedKeyByName[strings.ToLower(name)] = k
	}
}

// acceleratorKey 返回 accelerator 字符串里的键名（字母小写、命名词首字母大写）。
func acceleratorKey(k ui.Key) string { return keyNameByKey[k] }

// parseAccelerator 解析当前平台的 MyGo accelerator。
func parseAccelerator(s string) (keyBinding, error) {
	return parseAcceleratorForPlatform(s, runtime.GOOS)
}

// parseAcceleratorForPlatform 将 CmdOrCtrl 解析为目标平台的物理修饰键，
// 同时保留逻辑标记；显式 Super/Win 始终解析为 ui.Super。
func parseAcceleratorForPlatform(s, goos string) (keyBinding, error) {
	parts := strings.Split(s, "+")
	if strings.TrimSpace(s) == "" {
		return keyBinding{}, fmt.Errorf("%s", locales.AccEmpty())
	}
	var b keyBinding
	for i, raw := range parts {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			return keyBinding{}, fmt.Errorf("%s", locales.AccInvalid(s))
		}
		last := i == len(parts)-1
		if !last {
			switch p {
			case "cmdorctrl", "commandorcontrol":
				b.primary = true
				b.mods |= primaryModifier(goos)
				continue
			case "cmd", "command", "super", "meta", "win":
				b.mods |= ui.Super
				continue
			case "ctrl", "control":
				b.mods |= ui.Ctrl
				continue
			case "alt", "opt", "option":
				b.mods |= ui.Alt
				continue
			case "shift":
				b.mods |= ui.Shift
				continue
			}
			return keyBinding{}, fmt.Errorf("%s", locales.AccUnknownMod(raw))
		}
		if p == "plus" {
			b.key = ui.KeyEqual
			continue
		}
		key, ok := namedKeyByName[p]
		if !ok || len([]rune(p)) != 1 && key == ui.KeyUnknown {
			return keyBinding{}, fmt.Errorf("%s", locales.AccUnknownKey(raw))
		}
		b.key = key
	}
	if b.key == ui.KeyUnknown {
		return keyBinding{}, fmt.Errorf("%s", locales.AccMissingKey(s))
	}
	return b, nil
}

func keyBindingFromInput(mods ui.Modifiers, key ui.Key, goos string) keyBinding {
	return keyBinding{mods: mods, key: key, primary: mods&primaryModifier(goos) != 0}
}

// accelerator 输出当前平台的规范化 accelerator。
func (b keyBinding) accelerator() string {
	return b.acceleratorForPlatform(runtime.GOOS)
}

func (b keyBinding) acceleratorForPlatform(goos string) string {
	name := acceleratorKey(b.key)
	if name == "" {
		return ""
	}
	mods := b.mods
	var parts []string
	if b.primary {
		parts = append(parts, "CmdOrCtrl")
		mods &^= primaryModifier(goos)
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if len(name) == 1 {
		name = strings.ToUpper(name)
	}
	parts = append(parts, name)
	return strings.Join(parts, "+")
}

// normalizeLegacyAccelerator 兼容旧配置里 Super/Cmd 书写的热键
// （统一转成 CmdOrCtrl 规范形式）。
func normalizeLegacyAccelerator(s string) (string, error) {
	b, err := parseAccelerator(s)
	if err != nil {
		return "", err
	}
	if b.mods&ui.Super != 0 && !b.primary {
		b.mods &^= ui.Super
		b.mods |= primaryModifier(runtime.GOOS)
		b.primary = true
	}
	return b.accelerator(), nil
}

// sameBinding 按目标平台解析后的物理修饰键和主键比较。
func sameBinding(a, b keyBinding) bool { return a.mods == b.mods && a.key == b.key }

// ---- 显示名 ----

func (b keyBinding) displayName() string {
	return b.displayNameForPlatform(runtime.GOOS)
}

func (b keyBinding) displayNameForPlatform(goos string) string {
	var sb strings.Builder
	if goos == "darwin" {
		if b.mods&ui.Super != 0 {
			sb.WriteRune('⌘')
		}
		if b.mods&ui.Ctrl != 0 {
			sb.WriteRune('⌃')
		}
		if b.mods&ui.Alt != 0 {
			sb.WriteRune('⌥')
		}
		if b.mods&ui.Shift != 0 {
			sb.WriteRune('⇧')
		}
		sb.WriteString(displayKeyName(b.key))
		return sb.String()
	}
	if b.mods&ui.Super != 0 {
		sb.WriteString("Win+")
	}
	if b.mods&ui.Ctrl != 0 {
		sb.WriteString("Ctrl+")
	}
	if b.mods&ui.Alt != 0 {
		sb.WriteString("Alt+")
	}
	if b.mods&ui.Shift != 0 {
		sb.WriteString("Shift+")
	}
	sb.WriteString(displayKeyNameForPlatform(b.key, goos))
	return sb.String()
}

func displayKeyNameForPlatform(k ui.Key, goos string) string {
	if goos == "darwin" {
		return displayKeyName(k)
	}
	switch k {
	case ui.KeySpace:
		return locales.KeySpace()
	case ui.KeyEnter:
		return locales.KeyEnter()
	case ui.KeyEscape:
		return "Esc"
	case ui.KeyBackspace:
		return locales.KeyBackspace()
	case ui.KeyTab:
		return "Tab"
	case ui.KeyDelete:
		return "Delete"
	}
	return displayKeyName(k)
}

func displayKeyName(k ui.Key) string {
	switch k {
	case ui.KeySpace:
		return locales.KeySpace()
	case ui.KeyEnter:
		return locales.KeyEnter()
	case ui.KeyEscape:
		return "Esc"
	case ui.KeyBackspace:
		return locales.KeyBackspace()
	case ui.KeyTab:
		return "Tab"
	case ui.KeyDelete:
		return "⌦"
	case ui.KeyHome:
		return "↖"
	case ui.KeyEnd:
		return "↘"
	case ui.KeyPageUp:
		return "⇞"
	case ui.KeyPageDown:
		return "⇟"
	case ui.KeyLeft:
		return "←"
	case ui.KeyRight:
		return "→"
	case ui.KeyUp:
		return "↑"
	case ui.KeyDown:
		return "↓"
	}
	if name := acceleratorKey(k); name != "" {
		if len(name) == 1 {
			return strings.ToUpper(name)
		}
		return name
	}
	return "?"
}

// ---- 命令定义 ----

// commandDef 是一条可绑定命令。执行统一走 runFormatCommand(id)，
// 避免 registry 在包初始化期引用执行器形成环。
type commandDef struct {
	id     string
	name   string // 显示名（Keymap 与菜单共用）
	group  string // keymap 分组：inline / heading / blocks / insert / global
	def    string // 默认 accelerator（""= 默认未绑定）
	global bool   // 全局命令（后台可触发）而非文档命令
}

// keymapGroups 决定 Keymap 页与 Markdown 菜单的分组顺序。
// keymapGroupName 返回分组的当前语言显示名（顺序见 keymapGroupIDs）。
func keymapGroupName(id string) string {
	switch id {
	case "inline":
		return locales.GroupInline()
	case "heading":
		return locales.GroupHeading()
	case "blocks":
		return locales.GroupBlocks()
	case "insert":
		return locales.GroupInsert()
	case "global":
		return locales.GroupGlobal()
	}
	return id
}

// keymapGroupIDs 决定 Keymap 页与 Markdown 菜单的分组顺序。
var keymapGroupIDs = []string{"inline", "heading", "blocks", "insert", "global"}

var commandRegistry = []commandDef{
	{id: cmdBold, name: "cmdBold", group: "inline", def: "CmdOrCtrl+B"},
	{id: cmdItalic, name: "cmdItalic", group: "inline", def: "CmdOrCtrl+I"},
	{id: cmdInlineCode, name: "cmdInlineCode", group: "inline", def: "CmdOrCtrl+Shift+E"},
	{id: cmdLink, name: "cmdLink", group: "inline", def: "CmdOrCtrl+K"},
	{id: cmdH1, name: "cmdH1", group: "heading", def: ""},
	{id: cmdH2, name: "cmdH2", group: "heading", def: ""},
	{id: cmdH3, name: "cmdH3", group: "heading", def: ""},
	{id: cmdH4, name: "cmdH4", group: "heading", def: ""},
	{id: cmdH5, name: "cmdH5", group: "heading", def: ""},
	{id: cmdH6, name: "cmdH6", group: "heading", def: ""},
	{id: cmdBody, name: "cmdBody", group: "heading", def: ""},
	// 标签与标题的数字键分工：标签统一占用主修饰键+数字（⌘N / Ctrl+N，
	// 浏览器惯例）；标题退到各平台次级修饰键（macOS ⌃N，其他平台 Alt+N，
	// 见 defaultBindingForPlatform），默认串在注册表里留空。
	{id: cmdQuote, name: "cmdQuote", group: "blocks", def: "CmdOrCtrl+Shift+Q"},
	{id: cmdBullet, name: "cmdBullet", group: "blocks", def: "CmdOrCtrl+Shift+8"},
	{id: cmdOrdered, name: "cmdOrdered", group: "blocks", def: "CmdOrCtrl+Shift+7"},
	{id: cmdTask, name: "cmdTask", group: "blocks", def: "CmdOrCtrl+Shift+9"},
	{id: cmdTaskTime, name: "cmdTaskTime", group: "blocks", def: "CmdOrCtrl+Shift+0"},
	{id: cmdCodeBlock, name: "cmdCodeBlock", group: "insert", def: "CmdOrCtrl+Alt+C"},
	{id: cmdRule, name: "cmdRule", group: "insert", def: ""},
	{id: cmdTable, name: "cmdTable", group: "insert", def: ""},
	{id: cmdToggleWindow, name: "cmdToggleWindow", group: "global", def: "CmdOrCtrl+Shift+Space", global: true},
	{id: cmdAssistant, name: "cmdAssistant", group: "global", def: "CmdOrCtrl+Shift+A"},
	{id: cmdAssistantFocus, name: "cmdAssistantFocus", group: "global", def: "CmdOrCtrl+J"},
}

// commandDisplayName 命令名注册表存 catalog 键，显示时按当前语言取文案。
func commandDisplayName(def commandDef) string {
	switch def.name {
	case "cmdBold":
		return locales.CmdBold()
	case "cmdItalic":
		return locales.CmdItalic()
	case "cmdInlineCode":
		return locales.CmdInlineCode()
	case "cmdLink":
		return locales.CmdLink()
	case "cmdBody":
		return locales.CmdBody()
	case "cmdQuote":
		return locales.CmdQuote()
	case "cmdBullet":
		return locales.CmdBullet()
	case "cmdOrdered":
		return locales.CmdOrdered()
	case "cmdTask":
		return locales.CmdTask()
	case "cmdTaskTime":
		return locales.CmdTaskTime()
	case "cmdCodeBlock":
		return locales.CmdCodeBlock()
	case "cmdRule":
		return locales.CmdRule()
	case "cmdTable":
		return locales.CmdTable()
	case "cmdToggleWindow":
		return locales.CmdToggleWindow()
	case "cmdAssistant":
		return locales.CmdAssistant()
	case "cmdAssistantFocus":
		return locales.CmdAssistantFocus()
	}
	if n := def.name; strings.HasPrefix(n, "cmdH") {
		if v := n[len("cmdH"):]; v >= "1" && v <= "9" {
			return locales.CmdH(int(v[0] - '0'))
		}
	}
	return def.name
}

func commandDefByID(id string) (commandDef, bool) {
	for _, def := range commandRegistry {
		if def.id == id {
			return def, true
		}
	}
	return commandDef{}, false
}

// ---- 固定绑定（不改键） ----

// fixedBindingsForPlatform 是不开放改键的既有快捷键：Keymap 录键时作为冲突源，
// 让「应用命令与全局命令统一校验」有完整依据，也是窗口兑底键位的来源之一。
// 标签数字键两平台统一为主修饰键+数字（⌘N / Ctrl+N，浏览器惯例）；隐藏应用
// 是 macOS 独有菜单项，其他平台不入表。fixedBindingKey 是固定绑定显示名的
// catalog 键；显示经 fixedBindingName 取当前语言。
func fixedBindingsForPlatform(goos string) []struct{ acc, key string } {
	list := []struct{ acc, key string }{
		{"CmdOrCtrl+F", "find"},
		{"Shift+CmdOrCtrl+F", "findAllTabs"},
		{"CmdOrCtrl+G", "findNext"},
		{"Shift+CmdOrCtrl+G", "findPrev"},
		{"Shift+CmdOrCtrl+P", "sourceMode"},
		{"Alt+CmdOrCtrl+T", "keepOnTop"},
		{"CmdOrCtrl+=", "zoomIn"},
		{"CmdOrCtrl+-", "zoomOut"},
		{"CmdOrCtrl+0", "zoomReset"},
		{"CmdOrCtrl+1", "tab1"}, {"CmdOrCtrl+2", "tab2"}, {"CmdOrCtrl+3", "tab3"},
		{"CmdOrCtrl+4", "tab4"}, {"CmdOrCtrl+5", "tab5"}, {"CmdOrCtrl+6", "tab6"},
		{"CmdOrCtrl+7", "tab7"}, {"CmdOrCtrl+8", "tab8"}, {"CmdOrCtrl+9", "tab9"},
		{"CmdOrCtrl+T", "newTab"},
		{"CmdOrCtrl+R", "renameTab"},
		// 删除标签不用 ⌘W（已是关闭窗口）也不用 ⌥⌘D（macOS 系统占给 Dock
		// 自动隐藏，系统层先消费）；⌘⇧ 与 ⌘⇧C（复制标签全文）同级。
		{"Shift+CmdOrCtrl+D", "deleteTab"},
		{"Alt+CmdOrCtrl+Left", "prevTab"},
		{"Alt+CmdOrCtrl+Right", "nextTab"},
		{"CmdOrCtrl+S", "save"},
		{"Shift+CmdOrCtrl+C", "copyTabText"},
		{"CmdOrCtrl+Z", "undo"},
		{"Shift+CmdOrCtrl+Z", "redo"},
		{"CmdOrCtrl+X", "cut"},
		{"CmdOrCtrl+C", "copy"},
		{"CmdOrCtrl+V", "paste"},
		{"CmdOrCtrl+A", "selectAll"},
		{"CmdOrCtrl+Q", "quit"},
		{"CmdOrCtrl+W", "closeWindow"},
		{"CmdOrCtrl+M", "minimize"},
		{"CmdOrCtrl+,", "settings"},
		{"CmdOrCtrl+Tab", "appSwitcher"},
	}
	if goos == "darwin" {
		// ⌘H 隐藏应用只存在于 macOS 菜单（RoleHide），其他平台不占键位。
		list = append(list, struct{ acc, key string }{"CmdOrCtrl+H", "hideApp"})
	}
	return list
}

// fixedBindingByKey 按 catalog 键取当前平台的固定绑定 accelerator（测试与展示共用）。
func fixedBindingByKey(key string) string {
	return fixedBindingByKeyForPlatform(key, runtime.GOOS)
}

func fixedBindingByKeyForPlatform(key, goos string) string {
	for _, fixed := range fixedBindingsForPlatform(goos) {
		if fixed.key == key {
			return fixed.acc
		}
	}
	return ""
}

// fixedBindingName 按 catalog 键给出固定绑定的当前语言显示名。
func fixedBindingName(key string) string {
	switch key {
	case "find":
		return locales.FixedFind()
	case "findAllTabs":
		return locales.MenuFindAllTabs()
	case "findNext":
		return locales.FixedFindNext()
	case "findPrev":
		return locales.FixedFindPrev()
	case "sourceMode":
		return locales.FixedSourceMode()
	case "keepOnTop":
		return locales.FixedKeepOnTop()
	case "zoomIn":
		return locales.MenuZoomIn()
	case "zoomOut":
		return locales.MenuZoomOut()
	case "zoomReset":
		return locales.MenuZoomReset()
	case "newTab":
		return locales.FixedNewTab()
	case "renameTab":
		return locales.FixedRenameTab()
	case "deleteTab":
		return locales.FixedDeleteTab()
	case "prevTab":
		return locales.FixedPrevTab()
	case "nextTab":
		return locales.FixedNextTab()
	case "save":
		return locales.FixedSave()
	case "copyTabText":
		return locales.FixedCopyTabText()
	case "undo":
		return locales.FixedUndo()
	case "redo":
		return locales.FixedRedo()
	case "cut":
		return locales.FixedCut()
	case "copy":
		return locales.FixedCopy()
	case "paste":
		return locales.FixedPaste()
	case "selectAll":
		return locales.FixedSelectAll()
	case "quit":
		return locales.FixedQuit()
	case "closeWindow":
		return locales.FixedCloseWindow()
	case "minimize":
		return locales.FixedMinimize()
	case "hideApp":
		return locales.FixedHideApp(appName)
	case "settings":
		return locales.FixedSettings()
	case "appSwitcher":
		return locales.FixedAppSwitcher()
	}
	if strings.HasPrefix(key, "tab") {
		if n := key[len("tab"):]; n >= "1" && n <= "9" {
			return locales.FixedSwitchToTab(int(n[0] - '0'))
		}
	}
	return key
}

// parsedFixedBinding 按固定键 catalog 键取当前平台解析后的绑定；
// 右键菜单项展示快捷键用。
func parsedFixedBinding(key string) (keyBinding, bool) {
	acc := fixedBindingByKey(key)
	if acc == "" {
		return keyBinding{}, false
	}
	b, err := parseAccelerator(acc)
	if err != nil {
		return keyBinding{}, false
	}
	return b, true
}
