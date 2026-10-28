package app

// 标签：从固定 7 个圆点升级为可管理的动态标签栏。
//
//   - tab 是一条标签记录：稳定 ID、可选名称、Markdown 文本、颜色。
//   - 默认 7 个标签，最多 MaxTabCount 个；至少保留 1 个。
//   - 访问与显示集中在 app 方法上（activeTab/tabTitle/tabColor…），
//     编辑器、状态栏和顶栏不各自解释标签。

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zakki/core/model"
	"zakki/locales"
)

const (
	// DefaultTabCount 是新建数据时的标签数量（Tot 的 7 个点）。
	DefaultTabCount = model.DefaultTabCount
	// MaxTabCount 是标签数量上限；数字快捷键 ⌘N / Ctrl+N 恰好全覆盖。
	MaxTabCount = model.MaxTabCount
	// DotCount 是固定 7 点时代的兼容别名（迁移与旧测试用）。
	DotCount = DefaultTabCount
)

// tabPaletteColors 是内置调色板：前 7 色沿用 Tot 的点颜色，
// 后 5 色供用户新增标签时选用。颜色是数据（存在 tab.Color），
// 无效或缺失时按槽位回退到调色板。
var tabPaletteColors = [12]string{
	"#d4253a", // 1 红（Tot）
	"#f67500", // 2 橙（Tot）
	"#ffd01c", // 3 黄（Tot）
	"#65c802", // 4 绿（Tot）
	"#0f75e9", // 5 蓝（Tot）
	"#a40bd8", // 6 紫（Tot）
	"#98989d", // 7 灰（Tot default）
	"#00a3b4", // 8 青
	"#f7429c", // 9 粉
	"#a2845e", // 10 棕
	"#5856d6", // 11 靛
	"#32ade6", // 12 天蓝
}

// tab 是一条标签记录。ID 稳定标识（新增/删除/排序后不变），
// UI key 与动画目标都用 ID 而非数组下标。
type tab struct {
	ID    uint64
	Name  string
	Text  string
	Color string
}

// ---- 标签访问（主线程） ----

// activeTab 当前标签；调用方保证 len(a.tabs) > 0（迁移与操作都维持）。
func (a *app) activeTab() *tab { return &a.tabs[a.active] }

// tabTitle 第 i 个标签的名称原文；未命名为空串。
func (a *app) tabTitle(i int) string { return a.tabs[i].Name }

// displayTitle 第 i 个标签的展示标题：仅命名标签返回名称（去首尾空白），
// 未命名返回空串（顶栏只显示圆点）。
func (a *app) displayTitle(i int) string { return strings.TrimSpace(a.tabs[i].Name) }

// menuTitle 菜单里的标签名：命名用标题，未命名用「标签 N」。
func (a *app) menuTitle(i int) string {
	if name := a.displayTitle(i); name != "" {
		return name
	}
	return locales.TabN(fmt.Sprint(i + 1))
}

// tabTooltip 顶栏圆点的悬停提示：标签名 + 切换快捷键。
func (a *app) tabTooltip(i int) string {
	tip := a.menuTitle(i)
	if b, ok := parsedFixedBinding(fmt.Sprintf("tab%d", i+1)); ok {
		tip += "  " + b.displayName()
	}
	return tip
}

// validTabColor 校验标签颜色：#rgb/#rrggbb 形式（ui.Hex 只接受这些
// 且非法会 panic，所以存盘前必须先校验）。
func validTabColor(s string) bool {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 3 && len(h) != 6 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// tabColor 第 i 个标签的颜色：记录颜色优先，无效/缺失回退内置调色板。
func (a *app) tabColor(i int) ui.Color {
	if validTabColor(a.tabs[i].Color) {
		return ui.Hex(a.tabs[i].Color)
	}
	return ui.Hex(tabPaletteColors[i%len(tabPaletteColors)])
}

// tabAccent 标签的强调色（焦点边框、勾选框、链接）：槽位在前 7 且用的
// 是 Tot 默认色时沿用 Tot 的明暗 accent；自定义色向黑白收敛保证可读。
func (a *app) tabAccent(i int, dark bool) ui.Color {
	if i < len(dotThemes) && (!validTabColor(a.tabs[i].Color) || strings.EqualFold(a.tabs[i].Color, tabPaletteColors[i])) {
		return dotAccent(i, dark)
	}
	c := a.tabColor(i)
	if dark {
		return c.Mix(ui.RGB(255, 255, 255), 0.35)
	}
	return c.Mix(ui.RGB(0, 0, 0), 0.30)
}

// unusedTabColor 未被现有标签占用的第一个调色板色（新增标签用）。
func (a *app) unusedTabColor() string {
	for _, cand := range tabPaletteColors {
		used := false
		for i := range a.tabs {
			if strings.EqualFold(a.tabs[i].Color, cand) {
				used = true
				break
			}
		}
		if !used {
			return cand
		}
	}
	return tabPaletteColors[len(a.tabs)%len(tabPaletteColors)]
}

// clampActive 把 active 收敛到有效范围（删除/迁移后调用）。
// 调用方保证 len(a.tabs) > 0。
func (a *app) clampActive() {
	a.active = clamp(a.active, 0, len(a.tabs)-1)
}

// ---- 标签操作（主线程，操作后统一走 afterTabsChanged） ----

// defaultTabs 7 个默认标签（空文本、原 7 色、ID 1..7）。
// 首次运行的欢迎文本由 initDefaults 单独填进标签 1。
func defaultTabs() []tab {
	tabs := make([]tab, DefaultTabCount)
	for i := range tabs {
		tabs[i] = tab{ID: uint64(i + 1), Color: tabPaletteColors[i]}
	}
	return tabs
}

// nextID 分配下一个标签 ID。
func (a *app) nextID() uint64 {
	id := a.nextTabID
	a.nextTabID++
	return id
}

// syncNextTabID 让 ID 计数器越过现有最大 ID（载入/迁移后调用）。
func (a *app) syncNextTabID() {
	var maxID uint64
	for i := range a.tabs {
		if a.tabs[i].ID > maxID {
			maxID = a.tabs[i].ID
		}
	}
	if a.nextTabID <= maxID {
		a.nextTabID = maxID + 1
	}
}

// addTab 新增一个空标签并切换过去；达到上限返回 false。
// 自动分配未使用的调色板颜色与新 ID，随后聚焦内容编辑。
func (a *app) addTab() bool {
	if len(a.tabs) >= MaxTabCount {
		a.showToast(locales.TabMaxCannotAdd(MaxTabCount))
		return false
	}
	if a.cfg.Source {
		a.saveSourceSession()
	}
	a.tabs = append(a.tabs, tab{ID: a.nextID(), Color: a.unusedTabColor()})
	a.active = len(a.tabs) - 1
	a.afterTabsChanged(true)
	if a.cfg.Source {
		a.restoreSourceSession()
	} else {
		// 空标签也要有可聚焦的块编辑器：光标落在末尾，切过来即可输入。
		a.focusBlockEnd(len(a.mdBlocks()) - 1)
	}
	return true
}

// deleteTabNow 删除已确认的标签；至少保留一个，统一保存到当前笔记文件。
func (a *app) deleteTabNow(i int) bool {
	if len(a.tabs) <= 1 {
		a.showToast(locales.TabKeepOne())
		return false
	}
	if i < 0 || i >= len(a.tabs) {
		return false
	}
	removed := a.tabs[i].ID
	activeID := a.tabs[a.active].ID
	activeRemoved := removed == activeID
	if a.cfg.Source {
		a.saveSourceSession()
	}
	a.tabs = slices.Delete(a.tabs, i, i+1)
	delete(a.srcSessions, removed)
	if activeRemoved {
		a.active = min(i, len(a.tabs)-1)
	} else {
		for k := range a.tabs {
			if a.tabs[k].ID == activeID {
				a.active = k
				break
			}
		}
	}
	a.clampActive()
	a.cancelMorphFor(removed)
	a.afterTabsChanged(activeRemoved)
	if a.cfg.Source {
		a.restoreSourceSession()
	}
	return true
}

func (a *app) tabIndexByID(id uint64) int {
	for i := range a.tabs {
		if a.tabs[i].ID == id {
			return i
		}
	}
	return -1
}

// confirmDeleteTab UI 入口：有内容的标签先弹原生确认（居中于主窗口，
// 回车默认落在「取消」，危险操作不跟默认键），确认后按稳定 ID
// 重新定位，避免对话框期间重排标签后误删另一项。
func (a *app) confirmDeleteTab(i int) {
	if i < 0 || i >= len(a.tabs) || len(a.tabs) <= 1 {
		return
	}
	if strings.TrimSpace(a.tabs[i].Text) == "" {
		a.deleteTabNow(i)
		return
	}
	tabID := a.tabs[i].ID
	title := a.menuTitle(i)
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:        a.win,
			Type:          mygo.MessageQuestion,
			Title:         locales.DialogDeleteTabTitle(),
			Message:       locales.DialogDeleteTabMessage(title),
			Detail:        locales.DialogDeleteTabDetail(),
			Buttons:       []string{locales.DialogDeleteTabConfirm(), locales.CommonCancel()},
			DefaultButton: 1, // 回车给「取消」：删除必须显式确认
			CancelButton:  1,
		})
		if err != nil || res.Button != 0 {
			return
		}
		a.deleteConfirmedTab(tabID)
	}()
}

// deleteConfirmedTab 只把对话框创建前捕获的不可变 ID 投递给主窗口，后台
// 不读取标签切片。窗口生命周期与 ID 定位都在 editor lane 的 action 内复核。
func (a *app) deleteConfirmedTab(tabID uint64) {
	a.runOnMain(a.confirmedDeleteAction(tabID))
}

func (a *app) confirmedDeleteAction(tabID uint64) func() {
	return func() {
		if !a.winOpen {
			return
		}
		if current := a.tabIndexByID(tabID); current >= 0 {
			a.deleteTabNow(current)
		}
	}
}

// renameTab 命名第 i 个标签（去首尾空白；空串=未命名只显示圆点）。
func (a *app) renameTab(i int, name string) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.tabs[i].Name = strings.TrimSpace(name)
	a.rescheduleReminders()
	a.scheduleSave()
	a.rebuildTabMenus()
}

// moveTab 上/下移动第 i 个标签（dir=-1 上移，+1 下移）；
// active 跟随它原来指向的那个标签（按 ID）。
func (a *app) moveTab(i, dir int) bool {
	to := i - 1 // 上移：落到前一行之前
	if dir > 0 {
		to = i + 2 // 下移：落到后一行之后（to 语义是「移到哪一行之前」）
	}
	return a.reorderTab(i, to)
}

// reorderTab 把第 i 个标签移动到 to（List 语义：移到第 to 行之前，
// len(tabs) 表示末尾），供设置页拖拽排序与 moveTab 共用；
// active 跟随它原来指向的那个标签（按 ID）。
func (a *app) reorderTab(i, to int) bool {
	if i < 0 || i >= len(a.tabs) || to < 0 || to > len(a.tabs) {
		return false
	}
	j := to
	if j > i {
		j-- // 目标在后：剔除被移走的那行后再折算插入点
	}
	if j == i {
		return false
	}
	activeID := a.tabs[a.active].ID
	if a.cfg.Source {
		a.saveSourceSession()
	}
	tb := a.tabs[i]
	a.tabs = slices.Delete(a.tabs, i, i+1)
	a.tabs = slices.Insert(a.tabs, j, tb)
	for k := range a.tabs {
		if a.tabs[k].ID == activeID {
			a.active = k
			break
		}
	}
	a.afterTabsChanged(false)
	if a.cfg.Source {
		a.restoreSourceSession()
	}
	return true
}

// setTabColor 设置第 i 个标签颜色（校验失败忽略）。
func (a *app) setTabColor(i int, color string) {
	if i < 0 || i >= len(a.tabs) || !validTabColor(color) {
		return
	}
	a.tabs[i].Color = strings.ToLower(color)
	a.scheduleSave()
	a.refreshSettingsWindow()
	a.withWindow(func() {})
}

// beginRename 进入当前标签的重命名（双击色条或 ⌘R）：名字写在色条里的输入框，
// Enter 提交，Escape 取消，焦点离开等同提交。
func (a *app) beginRename(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	// 换标签重命名前先提交上一个，避免草稿被静默覆盖。
	if a.renaming != 0 && a.renaming != a.tabs[i].ID {
		a.commitRenameOnBlur()
	}
	a.renaming = a.tabs[i].ID
	a.renameDraft = a.tabs[i].Name
	a.renameFocus = 1
	a.renameSeenFocus = false
}

// commitRename 提交重命名并把焦点交回内容编辑（回车/Tab 路径）。
func (a *app) commitRename() {
	a.commitRenameCommon(true)
}

// commitRenameOnBlur 失焦提交（点击内容、其他标签或切走窗口）：等同回车，
// 但不抢回内容焦点——焦点已归用户点击处。
func (a *app) commitRenameOnBlur() {
	a.commitRenameCommon(false)
}

// commitRenameCommon 提交重命名（名称去首尾空白；空串 = 未命名只显示圆点）。
func (a *app) commitRenameCommon(focusContent bool) {
	if a.renaming == 0 {
		return
	}
	id, draft := a.renaming, a.renameDraft
	a.endRename()
	for i := range a.tabs {
		if a.tabs[i].ID == id {
			if a.tabs[i].Name != strings.TrimSpace(draft) {
				a.renameTab(i, draft)
			}
			break
		}
	}
	if focusContent {
		a.liveFocus = 1
	}
}

// cancelRename 取消重命名，不落盘。
func (a *app) cancelRename() {
	a.endRename()
	a.liveFocus = 1
}

// endRename 清除重命名会话状态。
func (a *app) endRename() {
	a.renaming, a.renameDraft, a.renameFocus = 0, "", 0
}

// handleRenameInput 重命名输入框按键：Enter/Tab 提交，Escape 取消；
// 其余事件（含输入法组合）交还原生输入框，不抢占中文输入。
func (a *app) handleRenameInput(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	switch ev.Key {
	case ui.KeyEnter:
		if a.renameComposing {
			return false // 组合中的 Enter 归输入法
		}
		a.commitRename()
		return true
	case ui.KeyTab:
		a.commitRename()
		return true
	case ui.KeyEscape:
		a.cancelRename()
		return true
	}
	return false
}

// switchTab 切换当前标签（原 switchDot 的动态版）：关查找条、
// 重置实时预览焦点、表格工具与格式意图，走自动保存。
// 液态动画在下一帧构建时从缓存的旧几何接管（见 tab_strip.go）。
// 切换后聚焦输入区域、光标落在文档末尾（⌘数字、点击标签与 ⌘⌥←/→ 共用），
// 切过来即可直接续写；源码模式不走这里落光标，沿用标签会话恢复选区。
func (a *app) switchTab(i int) {
	if i == a.active || i < 0 || i >= len(a.tabs) {
		return
	}
	if a.cfg.Source {
		a.saveSourceSession()
	}
	a.endRename()
	a.active = i
	a.findOpen, a.query, a.matchIdx = false, "", 0
	// 切标签回到预览态：不能默认把首行变成源码；选区由下面的末尾聚焦统一给出。
	a.resyncLive(-1)
	a.mdActive, a.mdDraft, a.keyHint = -1, "", 0
	a.mdVisible, a.mdProjection = "", liveProjection{}
	a.liveUndo, a.liveRedo, a.liveUndoPending = nil, nil, 0
	a.tableTools = tableTools{}
	a.mdPending, a.lastPendingCaret, a.pendingBlock = nil, 0, -1
	a.closeLinkTool()
	if a.cfg.Source {
		a.restoreSourceSession()
	} else {
		a.focusBlockEnd(len(a.mdBlocks()) - 1)
	}
	a.syncMenus()
	// 纯导航不写盘：active 随下次真实保存或退出最终保存顺带落盘。
}

// cycleTab 上/下一个标签并循环（⌘⌥←/→）。
func (a *app) cycleTab(dir int) {
	if len(a.tabs) == 0 {
		return
	}
	a.switchTab((a.active + dir + len(a.tabs)) % len(a.tabs))
}

// afterTabsChanged 标签增删/排序后的统一收尾：重置编辑会话、
// 重建菜单、防抖保存、刷新设置窗口与主窗口。撤销历史只在当前标签换了
// 之后清空：删别的标签或排序不改正在编辑的文档，撤销应照常可用。
func (a *app) afterTabsChanged(activeChanged bool) {
	a.endRename()
	a.findOpen, a.query, a.matchIdx = false, "", 0
	a.resyncLive(0)
	a.mdActive, a.mdDraft, a.keyHint = -1, "", 0
	a.mdVisible, a.mdProjection = "", liveProjection{}
	if activeChanged {
		a.liveUndo, a.liveRedo = nil, nil
	}
	a.liveUndoPending = 0
	a.tableTools = tableTools{}
	a.mdPending, a.lastPendingCaret, a.pendingBlock = nil, 0, -1
	a.closeLinkTool()
	a.rebuildTabMenus()
	a.rescheduleReminders()
	a.scheduleSave()
	a.refreshSettingsWindow()
	a.withWindow(func() {})
}
