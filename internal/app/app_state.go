package app

import (
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zakki/internal/notes"
	iprefs "zakki/internal/prefs"
)

// tabsState 标签数据与当前选中下标。
type tabsState struct {
	tabs      []tab
	active    int
	nextTabID uint64
}

// tabStripState 标签栏液态动画与重命名（主线程；详见 tab_strip.go/view.go）。
type tabStripState struct {
	morph            tabMorphState
	lastSlots        []tabSlotGeom
	lastBody         tabBody
	tabScroll        ui.ScrollState
	tabViewportWidth float32
	lastIDs          []uint64
	builtActiveID    uint64
	renaming         uint64
	renameDraft      string
	renameFocus      int
	renameComposing  bool
	// renameSeenFocus 重命名输入框曾真正持有焦点；置位后才允许失焦提交，
	// 避免自动聚焦落定前的首帧误判为失焦。
	renameSeenFocus bool
	// 右键菜单待执行动作（存稳定标签 ID；0 = 无）。Chosen 在选中后的下一帧
	// 报告，这里记下目标、帧末执行，避免构建期改标签集合。
	menuRenamePending uint64
	menuDeletePending uint64
	menuExportPending uint64
	dropPending       tabDrop
	animNow           func() time.Time
}

// findState 查找条与匹配跳转。
type findState struct {
	findOpen    bool
	query       string
	matchIdx    int
	jumpPending bool
	// findFocus 查找框的焦点绑定：1 = 查找框。
	findFocus int
	// findComposing 查找框上一帧是否在输入法组合中：组合中的 Enter 归输入法。
	findComposing bool
	// findAllTabs 跨标签查找：计数覆盖全部标签，上/下一个越过当前标签末尾时切到下一个有命中的标签。
	findAllTabs bool
	// tabHits 按标签 ID 缓存非当前标签的命中数（文本与查询都没变就不重扫）。
	tabHits map[uint64]tabHitCount
}

type tabHitCount struct {
	doc, query string
	n          int
}

// liveEditorState Typora 式实时预览编辑（live_edit.go / live_blocks.go）。
type liveEditorState struct {
	mdVisible          string
	mdProjection       liveProjection
	liveUndo, liveRedo []liveEdit
	liveUndoPending    int
	editorHandle       ui.Handle
	documentMenu       bool
	editMenuPending    string
	mdActive           int
	mdDraft            string
	fenceDraft         bool
	fenceDraftLine     int
	mdCaret            int
	mdSelEnd           int
	keyHint            ui.Key
	hintArmed          bool
	mdComposing        bool
	prevComposing      bool
	tableBackward      bool
	tableUndoPending   bool
	tableTools         tableTools
	autoTableBefore    string
	autoTableAfter     string
	autoTableLine      int
	autoTableCol       int
	autoTableCaret     int
	liveFocus          int
	selSyncStart       int
	selSyncEnd         int
	timePop            timePopoverState
	// selDoc 文档级全选：Cmd+A 选中全部块后，复制/剪切/删除/输入按
	// 文档区间语义在 app 层拦截，不进单块控件。selDocStart/End 为文档 rune 偏移。
	selDoc                 bool
	selDocStart, selDocEnd int
	// clipSvc 缓存窗口服务：文档级复制/剪切写剪贴板用（headless 测试安全）。
	clipSvc          ui.Services
	clipSvcOK        bool
	scrollActive     bool
	liveClickHandled bool
	// liveScroll 实时预览滚动位置：手账纸面画在视口上，按它对齐内容滚动。
	liveScroll     ui.ScrollState
	liveList       ui.ListState // 实时预览块列表的虚拟化状态（只建可见块）
	virt           liveVirtState
	previewCache   livePreviewCache
	mdCacheValid   bool
	mdCacheDoc     string
	mdCacheBlocks  []liveBlock
	mdCacheLines   []string
	mdCacheOffsets []int // 块首 rune 偏移（末位为全文宽度），见 rebuildRuneOffsets
	// 提交管线 hint：spliceDoc 刚拼好的新文档行切分，mdBlocks 直接复用免一次全文 Split。
	spliceHintDoc   string
	spliceHintLines []string
	// codeLang* 活动代码块右下角语言徽标的编辑态（live_codeblock.go）：
	// block 为正在改语言的块首行号（-1 无），text 是输入框绑定草稿，
	// synced 是草稿已同步到的块首行号（-1 强制重同步），focus 是 FocusBind
	// 的聚焦请求（块首行号+1，0 无），seenFocus 置位后才允许失焦提交
	// （同 tabStrip 的 renameSeenFocus 惯例）。
	codeLangBlock     int
	codeLangText      string
	codeLangSynced    int
	codeLangFocus     int
	codeLangHandle    ui.Handle
	codeLangSeenFocus bool
	// 全文派生量的缓存：键是文档串（与可选的查询串），每帧重建视图时避免全文扫描。
	statsCacheDoc     string
	statsCacheLines   int
	statsCacheWords   int
	statsCacheChars   int
	statsCacheTab     uint64
	statsCacheAt      time.Time
	statsRefreshAt    time.Time // 已预约的统计刷新时刻（armStatsRefresh）
	rangesCacheDoc    string
	rangesCacheQuery  string
	rangesCacheDark   bool
	rangesCacheAccent ui.Color
	rangesCacheCode   string // 代码配色方案 id，参与区间缓存键
	rangesCache       []ui.TextRange
	matchesCacheDoc   string
	matchesCacheQuery string
	matchesCache      []ui.TextRange
	lowerDoc          string
	lowerCache        string
}

// formatState 格式命令意图与链接浮层（live_format.go）。
type formatState struct {
	mdPending        map[spanKind]bool
	lastPendingCaret int
	pendingBlock     int
	pendingEditKind  editKind
	nextEditIsPaste  bool
	linkTool         linkToolState
}

// sourceEditorState 源码模式选区与撤销快照。
type sourceEditorState struct {
	srcCaret         int
	srcSelEnd        int
	srcSelSyncStart  int
	srcSelSyncEnd    int
	srcPrev          liveSnapshot
	srcComposing     bool
	srcComposeBefore liveSnapshot
	srcSessions      map[uint64]sourceSession
}

type sourceSession struct {
	caret, selEnd            int
	selSyncStart, selSyncEnd int
	prev                     liveSnapshot
}

// toastState 居中 toast 提示队列（view_toast.go）：动作回调在主线程排队，
// 主窗口下一次构建时消费并交给 MyGo toast 引擎展示，自动消失。
type toastState struct {
	pending []string
}

// desktopRefs 窗口、菜单、托盘、全局热键与状态栏提示。
type desktopRefs struct {
	win        *mygo.Window
	mainWindow atomic.Pointer[mygo.Window]
	// settingsWindow 是 settingsWin 的原子镜像：后台回调（同步通知等）
	// 在主窗口未开时经它解析设置窗，不在后台直接读主线程字段。
	settingsWindow atomic.Pointer[mygo.Window]
	winOpen        bool
	hidden         bool
	menu           *mygo.Menu
	miPreview      *mygo.MenuItem
	miFind         *mygo.MenuItem
	miFindNext     *mygo.MenuItem
	miFindPrev     *mygo.MenuItem
	miFindAllTabs  *mygo.MenuItem
	miTop          *mygo.MenuItem
	hotkeyAcc      string
	noticeHotkey   string
	notice         string
	tray           *mygo.Tray
}

// prefsRuntimeState 内存中的偏好及其一次性迁移结果。
type prefsRuntimeState struct {
	fonts fontRuntimeState
	prefs preferences
	// appliedLocale 已应用到 MyGo 的界面语言：与 prefs.Appearance.Language
	// 比对，避免每次偏好更新都重复 SetLocale。
	appliedLocale string
	storageMoving bool
	storageError  string
}

// settingsWindowState 设置窗口与草稿 UI。
type settingsWindowState struct {
	settingsWin  *mygo.Window
	settingsPage string
	settingsUI   settingsUIState
}

// app 是窗口展示的全部状态。它只能在主线程被改动；
// 其他 goroutine 通过 Window.Update 回到主线程。
// 领域字段经匿名嵌入提升，调用方仍可用 a.mdActive 等形式访问。
type app struct {
	tabsState
	tabStripState
	findState
	liveEditorState
	reminders reminderScheduler
	formatState
	sourceEditorState
	desktopRefs
	toasts toastState
	syncState
	settingsWindowState
	prefsRuntimeState
	onboarding onboardingState

	cfg   settings
	ready bool

	notes      *notes.Store
	now        func() time.Time
	prefsStore *iprefs.Store
}
