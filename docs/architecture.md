# Zakki 架构说明

面向后续维护者与 agent：先读本文再改代码。本文只描述稳定事实，具体行为以源码为准
（关键处标注了文件与函数）。项目是 [MyGo](https://github.com/egoist/mygo) 原生 Go UI
桌面应用，无 HTML/JS/webview，纯 Go 单二进制。

项目分两层：`internal/app` 是 UI 与应用装配（唯一允许依赖 MyGo 的 internal 包）；
`internal/` 其余包是纯逻辑与持久化，禁止依赖 MyGo 与 `internal/app`。根目录只有入口 `main.go`。

依赖通过标准 `vendor/` 固定。MyGo v0.5.1 的原生文字区间样式补丁在
`patches/mygo-text-range-background.patch`，行内对象尺寸与真实几何补丁在
`patches/mygo-textinput-objects.patch`，快捷键优先于编辑器词间移动的按键路由补丁在
`patches/mygo-shortcut-claimed-keys.patch`（⌘数字切标签后编辑器持有焦点，⌘⌥←/→
仍能循环切标签），日期面板列宽与行间距补丁在 `patches/mygo-calendar-spacing.patch`；刷新依赖使用 `make vendor`，维护约束见
[依赖补丁](../patches/README.md)。字体／斜体／字重进入原生布局，底色／颜色／下划线仅参与绘制；
区间合并、IME、选区和滚动仍复用输入控件，不另建文本布局。

## 模块职责

| 文件 | 职责 |
|---|---|
| `main.go` / `internal/app/lifecycle.go` | 入口 `app.Run()`：单实例、日志、macOS 菜单（应用/段落/格式/编辑/显示/窗口）、托盘、全局热键、退出与更新收尾；主窗 mac 为无边框自绘顶栏，Windows 用隐藏标题栏模式（`TitleBarStyle: hidden`）保留原生最小化/最大化/关闭（`StateKey: "main"`）、关窗驻留托盘、`OnActivate` 复用 `showWindow`。菜单项与快捷键细节见 `menu_commands.go`、`menu_edit.go` 与 [configuration.md](configuration.md)。 |

| `internal/app/updates.go` | 注册 MyGo `plugins/updater/native` 原生更新窗口（中文、默认手动检查）；应用菜单与托盘提供「检查更新…」。`finishQuit` 让更新重启也经过最终保存，失败则取消退出。更新公钥与 `gomeen/Zakki` 分发地址在 `mygo.json`；tag 经全量检查后分别构建 macOS universal 与 Windows x64，双平台均成功再汇总到草稿 Release；流程在 `.github/workflows/release.yml`，操作见 [release.md](release.md)。 |
| `internal/app/prefs_runtime.go` / `internal/app/prefs_bridge.go` | 偏好运行时集成：`loadPreferences`/`updatePreferences`；写盘委托 `prefs.Store`。类型别名在 `prefs_bridge.go`。 |
| `internal/app/notes_bridge.go` | 笔记磁盘类型别名（`settings`/`savedTab`/`savedFile` 等旧配置别名同在此文件）与 `migrateSavedFile`、`newNotesStore`。 |
| `internal/app/storage_file.go` / `internal/app/settings_storage.go` | 唯一笔记文件：`validateStorageDir`（目录可写探测）、`chooseStorageDirAsync`（后台弹目录选择器，经窗口 `Update` 回主线程）；`applyStorageDir` 运行期安全迁址——串行 `Relocate`（拒绝覆盖已有目标 → 写入并校验候选 → 偏好持久化成功才切换权威路径 → 删除旧文件，失败保留原件并如实报错，同一路径为 no-op）；「保存与同步」页展示文件路径、保存状态与 Git 开关。首启向导与设置页共用。
| `internal/app/onboarding.go` / `internal/app/onboarding_view.go` | 首启初始化向导（仅全新安装，`firstRun` = 数据文件不存在）：640×560 独立窗口（`StateKey: "onboarding-v2"`），两页草稿式确认——① 语言（English/简体中文，选择即时 `App.SetLocale` 并写偏好）、明暗、风格（标准/手账，写 `appearance.style`，不强制字体）、中文字体、英文字体（预选 JetBrains Mono）；② 初始标签数 1–9 与笔记保存位置（预填当前数据目录，可换，始终可见；完成后 `Relocate` 到所选文件夹的 `zakki.json` 并持久化 `storage.dataFile`）。选择不触发下载；主按钮显示「完成设置」或「下载字体」，提交后冻结草稿，缺失字体经 `prepareFonts` 统一下载（总进度+单字体进度），完成后停在就绪页，点「开始使用」才写偏好与初始数据（`commitFirstRunData` 语义，失败留在向导）并进入主窗口；失败可重试、返回修改（作废旧下载批次）或改用系统字体。关窗明确询问继续设置、使用默认设置或退出应用；未完成设置时选择退出或直接退出应用均不创建笔记，下次启动仍显示向导；欢迎文案随标签数生成（`welcomeTextFor`）；主窗口等向导结束再创建。 |

| `internal/atomicfile` | 笔记与偏好共用的持久写入：同目录随机临时文件 → 完整写入 → `Sync` → `Close` → rename → `SyncDir`；rename 后同步失败用 `CommitError` / `IsCommitted` 表明目标已替换。Windows 不执行目录句柄同步。 |
| `internal/sysnotify` | 系统通知投递（无 MyGo 依赖）：v1 为"应用内定时器到点 + 系统通知投递"（ADR-0001），macOS 走带 argv 的 osascript、Windows 走固定 AUMID 的 PowerShell WinRT toast（标题正文由环境变量传入、XML 文本节点构造，命令隐藏且 10 秒超时），点击通知无回调；`Notifier` 接口留待后续 UNUserNotificationCenter 升级。 |
| `core/` | 共享数据内核（嵌套 module `zakki/core`，零第三方依赖的叶子）：时间令牌（`@`/`~`/`^`）解析与规范写回、重复规则与实例展开、当前实例及完成推进（ADR-0002）、原子时区选择与 `RuleSyntaxVersion=1`；`core/zakkicore`（包名 `zakkicore`）提供 UTF-16 偏移及错误码的 gomobile JSON 边界。桌面 `internal/markdown` 与未来移动端从这一份实现消费语义；分层铁律见 [engineering-standard.md](engineering-standard.md) 第 3 节。 |
| `mobile/binding` / `mobile/ios` | 移动端脚手架（不参与桌面 `make check` 的根 module 构建与测试）：`mobile/binding` 是独立 Go module，`require zakki/core` 并 `replace => ../../core`，是唯一允许依赖 golang.org/x/mobile 的模块（tool directive 固定版本），供 `gomobile bind` 把 `core/zakkicore` 打成 XCFramework／AAR；`mobile/ios` 是 XcodeGen 工程（`project.yml` 生成 .xcodeproj，生成物与绑定产物不入库），不参与任何 Go 构建。core 仍是零依赖叶子。 |
| `internal/markdown` | Markdown 解析、格式变换、表格纯函数（无 MyGo 依赖）。时间令牌（`@时刻`、`~提前量`、`^重复`）的行内识别与原位更新（`timeinline.go`）与旧行尾解析（`rowtime.go`）已委托 `core`，本包只保留调用面（含 `ParseRule` / `Expand` / `Occurrence` / `Advance` 薄转发）并把行内代码/链接的 rune 保护区换算为 core 的字节保护区；`AlertTiers` 与弹层共用一份档位白名单。 |
| `internal/codehl` | 代码块逐行词法着色纯函数：`Kind`（注释/字符串/数字/关键字/函数/键）× 行内 `Span`，按围栏 info 归一化语言（go/js/ts/py/sh/json/yaml/toml/rust/c/java 家族，未知语言走通用规则）；不跨行追踪，块注释/多行字符串只着色起始行（无 MyGo 依赖）。 |
| `internal/notes` | `zakki.json` 磁盘模型与唯一权威文件的 `Store`：`store.go` 负载入（`Load` 返回文件存在性与原始 hadTabs，记住磁盘字节指纹；成功重读作废旧待写快照与重试）、写保护（损坏/高版本/标签超限/外部改动时拒写，不产生副本）与路径状态；`save.go` 负责 `ScheduleSave`（首笔修改固定截止、后续输入只换快照不延期）、`SaveNow`、退出 `Shutdown` 与失败自动重试；`relocate.go` 负责 `Relocate` 单文件安全迁址；`git_sync.go` / `git_schedule.go` 负责对这一个文件的精确路径 Git 提交与按间隔推送（后台单 worker、代号门禁、本地写命令与迁址互斥、推送不占锁）。数据版本 3：排版主题旧 ID 随载入迁移为 `note`。
| `internal/prefs` | `preferences.json` 模型、`Store`（修订号/防抖写盘/异常门禁）。偏好版本 13：新增 `appearance.padX` / `padY` 正文内边距覆盖；版本 12 新增 `storage.dataFile`（笔记文件绝对路径，空串 = 应用数据目录 `zakki.json`）与 `storage.saveIntervalMs`（默认 2000），新增 `git.autoCommit` / `git.autoPush`（默认关）与提交/推送间隔（默认 1 / 10 分钟），并移除 `backup` 整节与 `advanced.autosaveDelayMs`。版本 11：`advanced.windowColumns` / `windowRows` 取代像素宽高（`WindowPixels` / `WindowGrid` 换算）。版本 10：`appearance.style`（标准 / 手账），生效值经 `EffectiveColorTheme` / `EffectiveMarkdownTheme`。更早版本差异见 configuration.md。
| `locales/` | MyGo i18n 目录：`en.json`（源）+ `zh-Hans.json`，ICU MessageFormat；`go tool mygo generate` 生成 `catalog_gen.go`。`internal/app` 统一经 `locales.*` 取文案；语言选择在首启向导第一步、设置通用页与应用菜单，变更经 `App.SetLocale` 生效并由 `OnLocaleChanged` 重建菜单。 |
| `internal/app/datamigrate.go` | 数据目录解析：正式安装 `<系统应用数据>/Zakki`，开发运行 `Zakki Dev`（`resolveDataDir` 内 `sync.Once` 缓存，解析失败不构造 Store）。应用不扫描或迁移旧目录；当前目录或已配置路径的数据照常读取，找不到当前数据时才按全新安装处理。 |
| `internal/app/live_markdown.go` / `internal/app/view_highlight.go` | 桥接 `internal/markdown`；源码高亮 `editorRanges`（依赖 `ui.TextRange`），用每 rune 一位的占用位图判交，保留搜索优先与重叠候选整段拒绝；代码内容行按外观「代码配色」方案套 `internal/codehl` 区间。行内时间令牌在源码模式按 `@` 强调色、`~` 次级色分色，组件后的正文不染色。 |
| `internal/app/view_codehl.go` | 代码配色方案表 `codeSchemes`（柔和/明快 × 浅深）与解析 `codePaletteFor`；`codeLineSpans`（实时预览 RichText）与 `codeSourceRanges`（源码 TextRange）把 `internal/codehl` 词法片段套色。 |
| `internal/app/menu_commands.go` | 命令注册表 `commandRegistry`（ID/名称/分组/默认绑定，Markdown 命令 + 全局唤起）：菜单、Keymap 设置页、窗口兑底与编辑器共用；加速器解析/规范化/显示名（`parseAccelerator`/`accelerator`/`displayName`）、固定绑定表（`fixedBindingsForPlatform`）。 |
| `internal/app/keymap_bindings.go` | 改键校验与写入：平台默认键（`defaultBindingForPlatform`：标题退到次级修饰键 mac ⌃N / 其他平台 Alt+N）、录键校验（`validateBinding`）、冲突检测（`findBindingConflict`，含固定键与系统保留键）、单项写入/恢复与全部恢复。 |
| `internal/app/menu_edit.go` | 编辑命令路由：正文聚焦时，菜单栏与右键菜单的撤销、重做及剪贴板操作共用文档事务；搜索、标签重命名和设置中的输入保留系统编辑角色。焦点切换时 `refreshEditMenuRouting` 只刷新编辑子菜单项（系统 Role ↔ 应用 Click），不全量 `buildMenu`。右键命令在主视图帧末执行，避免控件构建期间重复记录历史。`copyTabTextToClipboard` 供菜单与快捷键兑底共用。 |
| `internal/app/live_format.go` | 格式命令执行（`execFormatCommand`）：标题/列表/引用/行内/代码块/插入在实时预览与源码模式共用；无选区切换「待输入格式」意图（`mdPending`，输入/粘贴/IME 确认时经 `pendingWrapFor` 生成有效 Markdown）；命令以独立撤销事务执行（`editCommand`）。 |
| `internal/app/live_link.go` | 链接浮层 `linkTool`：打开、校验并写回链接。 |
| `internal/app/settings_window.go` / `internal/app/settings_ui.go` / `internal/app/settings_storage.go` / `internal/app/settings_keymap.go` | 原生设置窗口：单实例非模态（`StateKey: "settings"`），四页导航（通用、标签、保存与同步、快捷键）与草稿绑定；变更经 `updatePreferences`；「保存与同步」页在 settings_storage.go（文件路径、保存状态与 Git 开关，见 storage_file.go 行）；快捷键页点击徽标录键、冲突就地「改用到这里」（录键期间经 `setKeymapRecording` 临时摘除菜单与全局热键，退出时重建；校验与写入在 keymap_bindings.go），全局热键先注册新键再注销旧键。字段契约见 [configuration.md](configuration.md)。 |
| `internal/app/settings_general.go` | 「通用」页骨架：常规（语言、窗口）在前，随后由 `settings_ui.go` 的 `appearanceSections` 构建主题、排版、代码、顶栏分区，整页共用受窗口高度约束的滚动区与单一重置入口；重置同时恢复窗口与外观，保留存储、Git、快捷键和笔记。默认窗口大小按每行字数 × 行数输入，附当前字号换算的像素。偏好异常的恢复入口保留在页顶。 |
| `internal/app/settings_select.go` | 设置页下拉：`boundedSelect` 与 `ui.Select` 同外观与键盘行为，弹层限高 300 并在内部滚动（`ui.Select` 不限高，选项多时被窗口裁掉）；`selectTrigger` / `selectPanel` 供字体选择器共用。 |
| `internal/app/view_theme.go` | 双层主题：`colorThemes`、`markdownLayouts`；经典颜色主题在主窗口与其他界面一样走中性调色板（`classicNeutralPalette`），`dotThemes` 只提供标签圆点与焦点 accent；状态栏仅小圆点保留标签色，正文按对比度选中性色、持久错误用主题危险色；`resolveStyle` 合并颜色 + 排版 + 用户覆盖，结果按（外观偏好、圆点、明暗、是否覆盖、本机字体列表身份）缓存最近 4 份（`fontRuntimeState.styles`），字体栈拼接与本机字体查找不再每帧重复；排版可带装饰尺寸（引用条宽、代码圆角/描边，手账主题使用）。字体栈在 Apple 字体之后列 Windows 回退（微软雅黑、宋体、Segoe UI、Consolas），macOS 渲染不变。 |
| `internal/app/view_sketch.go` | 手账排版的手绘装饰：种子确定的抖动铅笔线（`sketchLine`/`sketchRect`）、波浪分隔线、荧光笔标题底（一/二级）与铅笔下划线（三级及以下）；标题装饰容器 `sketchHeadingFrame` 在预览态与编辑态共用。点阵纸与页边红线、马克笔引用条、手绘勾选框（`sketchCheckbox`，保留复选框语义）与列表小圈；表格格线与代码框为铅笔线；手绘颜色主题内容区不透明（`paperBackground`）。 |
| `internal/app/window.go` | 窗口显示与隐藏：`showWindow`、`toggleWindow`、`applyAlwaysOnTop`；`minimumWindowSize` 按当前字号、行高与内边距保证至少 10 列、4 行正文并预留查找栏空间，创建窗口和偏好应用时同步原生最小尺寸；`defaultWindowSize` 按当前生效排版（`windowMetrics`：正文字号 / 行高 / 内边距覆盖或排版默认、平台顶栏 + 状态栏）把偏好里的每行字数 × 行数换算成新建主窗口的像素；创建主窗口时触发本机字体枚举（`loadFontFamilies`）；`resetWindowUI` 在创建新原生窗口前清理旧 `ListState` 和剪贴板服务，并恢复编辑器选区同步，文档与撤销历史保留。 |
| `internal/app/view_status.go` | 状态栏文案与字数统计：`statusWarn`（仅持久异常：保存/载入被暂停，一次性提示已改走 toast）、`countWords`、`saveLabel`；`docStats` 按文档串缓存，同一标签 32KB 以上的文档连续输入时每 250ms 至多重算一次，节流期沿用旧值并用 `time.AfterFunc` + `Window.Update` 预约一帧补算；切换标签立即重算。 |
| `internal/app/view_toast.go` | 居中 toast 提示：一次性操作反馈（标签上限/保底、导出成功、字体缓存异常、重读失败、退出保存失败）经 `showToast` 排队，主窗口每帧由 `toastViewport` 消费并交给 MyGo toast 引擎（`ToastViewportBase`/`ToastBase`）在窗口正中展示深色气泡，约 4 秒自动消失，悬停/失焦暂停计时，相同文案替换重新计时；持久异常不走 toast，仍由状态栏常驻展示。 |
| `internal/app/app_state.go` | `app` 由领域子结构体匿名嵌入，另加 `cfg`、`ready`、`now` 与 `notes.Store` / `prefs.Store`。 |
| `internal/app/state.go` | 笔记载入编排与搜索匹配；写盘委托 `notes.Store`。`seedInitialContent` 统一生成欢迎文本与「时间与提醒」使用说明：复用第二个初始标签，只初始化一个标签时合并到欢迎文本；不向已有标签补写。 |
| `internal/app/tab_model.go` | 动态标签：`tab`、`DefaultTabCount`（7）、`MaxTabCount`（9，数字快捷键全覆盖），以及当前标签的访问与增删改。`switchTab`/`addTab` 切换后聚焦末块输入区、光标在文档末尾（实时预览模式；源码模式沿用标签会话恢复选区）。 |
| `internal/app/tab_export.go` | 「导出为 Markdown…」（文件菜单与标签右键菜单）：主线程取快照（实时预览先 `commitDraft`），后台弹原生保存对话框，建议文件名取标签名并替换文件系统保留字符；经 `atomicfile` 原样写出，失败弹原生错误框，成功以居中 toast 提示。 |
| `internal/app/tab_strip.go` | 标签栏液态动画与重命名几何；标记只有圆点与圆环；当前标记的高亮度调色板色（黄、浅粉等）在浅色主题下向黑色收敛最多 18% 提高对比，深色与深色主题原样。未命名时全部同大；有名字时名字画在当前标签的色条里，背景与文字共用 `tab_palette.go` 的派生配色。有标题的补间只在标题完全淡出期间切换底色，字色不插值；连续切换不复活已淡出的标题，进入重命名时直接在目标位置就位。圆环描边且不参与液态桥接。`TabMotionOff` 关闭补间（与系统 ReduceMotion 同路径）。顶栏拖拽排序：圆点按钮 `Drag(tabDrag)`，标签条为落点，经过时画插入指示线，松手记入 `dropPending`、帧末经 `reorderTab` 重排（按下不动仍是点击，双击重命名与右键菜单不受影响）。标签右键菜单（重命名/前移/后移/导出/删除，与设置页、菜单栏和固定快捷键共用已有操作）：菜单动作记为待执行、主视图帧末按稳定 ID 重定位后执行（`runTabMenuPending`），不在构建期改标签集合；重命名先切到被右键的标签，与双击同路径。 |
| `internal/app/tab_palette.go` | 胶囊显示配色：优先柔白 / 墨黑，按 WCAG 相对亮度争取 5:1；背景最多向黑白混合 12%，不足时让文字向黑白极值收敛，稳定态不低于 4.5:1。仅派生显示色，不修改标签原色与持久快照；标题绘制和重命名输入共用配色。 |
| `internal/app/settings_tabs.go` | 设置窗口「标签」页：标签列表是可拖拽排序的 List（`ListState.Reorder` → `reorderTab`，active 按稳定 ID 跟随）：行首为与顶栏同形的小圆点（点开 `ui.Popover` + `ui.ColorPicker` 取色，含常用色板；草稿以 `tabWellSeeded` 基线防 `Changed` 迟到误回播种）、双击行内重命名（会话状态独立于顶栏胶囊重命名）与前移/后移、删除按钮；各按钮保留键盘焦点环。顶栏外观项（形状/标题宽/动画速度/动效/玻璃强度）在「通用」页的顶栏分区，见 settings_ui.go 的 `tabAppearanceSection`。 |
| `internal/app/view.go` | 主窗口骨架：`view` 每帧构建顶栏、查找条、源码编辑区与底栏；顶栏左侧「关闭窗口」调用 `Window.Hide`，保留笔记、标签和原生渲染资源，避免每次唤起都重建 Metal 管线；Dock、托盘或全局热键可重新显示。菜单关闭（⌘W）仍销毁窗口，后续由 `showWindow` 重建；退出流程不被隐藏逻辑拦截。顶栏按 `c.TitleBar()` 避让系统窗口按钮：右侧内边距加按钮带宽，Windows 上顶栏降到按钮带同高（32），内容与按钮 glyph 同轴垂直居中，无按钮带时保持 38。只有一个未命名标签且不在重命名时隐藏标签区（`tabStripHidden`）；单个命名标签仍显示标题与上下文菜单入口；并复位标签几何避免恢复多标签时残留补间。顶栏、底栏与查找条的图标按钮统一走 `chromeIcon`（16pt 图形、24pt 点击区、悬停浅底、名称兼作无障碍标签与悬停提示）；顶栏圆点悬停显示标签名与切换快捷键（`tabTooltip`）。底栏文字单行截断并带悬停全文，窄窗口下切换图标不被挤出。 |
| `internal/app/find_bar.go` | 查找条与命中跳转：输入框 + 计数 + 跨标签开关 + 上/下/关闭图标；`openFind(allTabs)` 由 ⌘F / ⇧⌘F 共用。跨标签模式下计数按标签缓存（`findState.tabHits`，键为文档内容 + 查询），上/下一个越过当前标签末尾时切到下一个有命中的标签。Enter 下一个、Shift+Enter 上一个（`findStep`，菜单查找下一个/上一个经 `findOpenOrStep` 共用）；`highlightQuery` 只在查找条打开时返回查询，源码区与实时预览（标题、段落、任务项，以及含命中的单行引用/列表）共用它高亮命中，关闭查找条即清除高亮。 |
| `internal/app/view_live.go` | 实时预览块列表：`livePane`（`ui.List` 虚拟化，只构建可见块；外层 Column 兜底左右留白与行间空隙的空白点击）、焦点块 `activeText` 与非焦点块渲染；列表显式绑定解析后的正文颜色、字体与行高，避免帧末恢复外框主题时覆盖内容继承。 |
| `internal/app/live_blocks.go` | 按行切块：`liveBlocks` / `parseLines`。焦点块保留排版并编辑可见文字；Markdown 标记留在源文本中。 |
| `internal/app/live_edit.go` | 草稿提交与焦点：`commitDraft`、`publishDraft`、`focusBlock`、块缓存与 rune 首偏移表。文档重切换代时清理预览派生缓存；偏移表重建分配新切片，不覆写当前构建帧持有的旧代。 |
| `internal/app/live_preview_cache.go` | 当前文档块代际的有限缓存（最多 64 块）：可见文字投影、表格 cell/偏移与代码 token spans；只保存纯数据，不保存 UI 元素/Context，不保留历史版本。切标签、重切和编辑失效，代码颜色随完整 palette 失效；仍由原控件整体布局单个大块。 |
| `internal/app/live_click.go` | 预览点击落点定位：文字元素自己接收点击（`bindPreviewClick`），`liveHit` 记录块号、`ui.Handle` 与文字投影，元素只活一帧，点击回调里再 Resolve；文字铺满行宽，点在第 n 行右侧空白时光标停到该行末尾而非整块末尾，行间距/页边用同一元素上一帧盒子换算。 |
| `internal/app/live_input.go` | 边界键：Enter 分段与列表续行、退格合块、方向键跨块；代码块内 Enter 委托 `applyCodeEnter`。 |
| `internal/app/live_codeblock.go` | 围栏代码块专属编辑：Tab/Shift+Tab 缩进（`applyCodeTab`，单位取通用页代码分区的 `appearance.codeIndent`，退化围栏落到合成内容行）、Enter 续行缩进（`applyCodeEnter`，继承行首缩进、开括号再加一级）、右下角语言徽标（`codeLangBadge`：FocusBind 聚焦、失焦/Enter 提交、Esc 取消，改写围栏 info 串不经 `applyDocEditAt` 以免抢主编辑器焦点；会话态 `codeLang*` 存 `liveEditorState`）。 |
| `internal/app/live_virtual.go` | 块内虚拟化：200 行以上的代码块预览与表格（含正在编辑的表格）只构建视口上下各一屏的行，其余用占位撑开；位置取上一帧 `Bounds` 并按滚动量校正，行高按构建过的行实测缓存（`liveVirtState`，键为块源码 + 块类型 + 参与布局的字体、字号、行高、表格 padding 与排版变体，帧末清掉未用条目），未测行用实测均值；正在编辑的表格行总会构建；窗口依据过期（首帧或刚滚动）时再要一帧校正。单个超长段落仍整段排版。 |
| `internal/app/live_task.go` | 普通与手账复选框共用 `toggleTask`：未勾选的合法重复任务经 `Advance` 推进锚点，保留提醒和规则且保持未勾选；序列结束才勾选，取消勾选沿用普通切换；一次文档事务支持撤销/重做。 |
| `internal/app/live_widgets.go` | 焦点块输入（`applyProjectedHint`）与表格单元格编辑控件。 |
| `internal/app/live_timechip.go` / `live_timeprojection.go` / `live_timeview.go` | 时间组件的文案、状态与行内对象呈现：预览和聚焦保持同一组件位置与样式，`liveProjection` 中单个 U+FFFC 对应完整时间源码。原生对象尺寸参与换行与光标布局；前后均可输入，组件后正文保留原位。合法重复任务的 chip 经 `Occurrence` 显示当前实例，当天即使过点仍显示今天，过去或结束实例保持中性、不标逾期。首次点击进入条目时光标默认在条目末尾；搜索选区触及令牌时扩展到整个组件。源码模式仍可直接修改令牌。 |
| `internal/app/live_timepopover.go` / `live_timerecur.go` | 时间弹层：DateInput 与 TimeInput 共享草稿时间，日期变更也启用完成；重复选择器经内核 `ParseRule` 播种预设或自定义（间隔、单位、每周周几及可选结束日期），预设切换同步自定义字段，未改规则保留 `^` 原文，选择「无」清除重复；软换行块只更新令牌所在源码行；点击时刻浮出可滚动的小时／分钟选择面板（12 小时制另有上下午列），输入控件仍可键盘操作；提醒档位 pill（`markdown.AlertTiers` 白名单，自动换行）、清除与完成；写回走勾选切换同一条管线并进撤销。`⌘⇧0`（list.taskTime）或点击 chip 打开；行尾键入 `@` 即时唤起（占位符就地转化，对齐表格即时转换心智，代码块与行中不触发）；源码模式无锚点不响应。 |
| `internal/app/reminders.go` | 提醒调度：主线程只截取全部标签的 ID/标题/文本快照，250ms 防抖后台按标签文本缓存解析块；编辑立即作废旧代。首次启动仅排未来触发；运行期新提醒提前量超过剩余时间时立即投递，已排未投递项迟到不超过截止后 60 秒仍投递；30 秒墙钟巡检并重排，以标签 ID、实例截止、提前量、正文 hash 去重；编辑前已排未投递资格按标签 ID、实例截止、提前量继承，保证正文变化跨截止仍用最新正文投递。已投递记录保留 24 小时。删除、改名、重读成功后重排；退出失败恢复定时器与巡检，成功退出停止投递。合法 `^` 经 `markdown.Expand` 展开未来 60 天、最多 64 个实例，每实例独立计算截止减提前量；解析块按文本缓存，实例每次按墙钟重展，30 秒巡检直接消费最近的不可变标签快照，主窗口销毁后仍续排；另保留一分钟迟到窗口交由既有排定资格门禁判断。状态受 `reminderScheduler` mutex 保护。 |
| `internal/app/live_inline_style.go` | 行内样式唯一映射 `inlineStyle`：粗体、斜体、代码字体／底色、链接颜色／下划线；投影预览、列表／引用 RichText 子元素与原生编辑区间共用定义。 |
| `internal/app/live_projection.go` | 可见文字投影与 rune 映射（`projectBlock`/`replaceSelection`）；`textRanges` 将共用行内样式转换为原生编辑区间，搜索命中（`searchHit`：强调色 + 加粗 + 强调色淡底）覆盖颜色、字重与底色，保留字体、斜体与装饰；文档级撤销事务（`liveEdit` + `editKind`：连续输入合并、粘贴/IME/格式命令独立），`undoLive` 覆盖实时预览与源码模式，恢复文本与选区。 |
| `internal/app/live_selection.go` | 文档级全选（`selectDocWide`/`menuSelectAll`）：跨块选区高亮与复制/剪切/删除/输入在 app 层处理；表格单元格内保留单元格原生全选。 |
| `internal/app/live_history.go` | 大文档撤销差分：64KiB 起将非栈顶的小改动存为克隆的字节差分；整篇替换仍共享原快照，避免反向放大内存。撤销/重做各自栈顶保留完整快照做精确文本校验，用已验证的相邻事务展开下一个栈顶；不靠整文档 hash、不减少 200 条历史、不扩大输入合并范围。差分发布后不可变；弹栈清零底层槽以释放全文引用。撤销历史只属于当前标签的这段编辑：切换到别的标签（含新增标签、删除当前标签）即清空，不按标签保留，也不写盘；删除其他标签或排序不影响当前历史。源码模式的标签会话（`sourceSession`）只记光标与选区。 |
| `internal/app/table_edit.go` | 表格编辑：`createTableOnEnter`、`navigateTable`、`tableEnter`。 |
| `internal/app/table_actions.go` | 表格工具状态 `tableTools` 与行列增删。 |
| `internal/app/table_toolbar.go` | 表格悬浮工具栏。 |
| `internal/app/live_syntax.go` | 块级类型与 `parseBlocks`（测试/预览适配）；行内解析与格式变换见 `internal/markdown`。 |
| `internal/app/view_icons.go` | 内嵌 Lucide 图标（ISC 许可），24×24 viewBox、2px 圆头描边，`ui.MustParseSVG` 生成。 |
| `internal/app/tray_icon.go` / `internal/app/assets/tray.png` / `internal/app/assets/tray-black.png` | 托盘图标：内嵌「墨 Z」手写字母 PNG（`trayPNG`，36×36px，与应用图标配套，白墨/黑墨两版）；macOS 以模板自动适配菜单栏明暗，其他平台系统不管模板，按桌面明暗选墨色并随 `Theme.OnUpdated` 切换，避免图标隐入同色任务栏。`assets/tray.svg` 保留矢量源。 |
| `internal/app/shortcut_fallback.go` | 窗口级快捷键兑底：每帧从统一键位表（命令绑定 + 固定键 + 用户覆盖）注册 `Context.Shortcut`，动作与菜单 Click 同一实现。macOS 菜单 accelerator 在系统层先消费、兑底不触发；Windows 原生 surface 不触发菜单 accelerator，兑底即实际触发路径。编辑角色六键由 ui 引擎内建处理不重复注册。 |
| `internal/app/shortcut_text.go` | 静态快捷键文案的平台转换：locale 字符串以 macOS 符号书写，非 macOS 展示前经 `shortcutText` 转成 Ctrl/Alt/Shift（⌃ 即 mac 标题键映射为 Alt）；动态键位展示走 `keyBinding.displayNameForPlatform`。 |
| `internal/app/font_runtime.go` / `internal/app/font_picker.go` | 字体运行时与选择控件：后台 `ui.FontFamilies()` 枚举本机字体并与远程目录去重（本机字体以 `local:<家族名>` 键入选项，经 `fonts.ClassifyFamily` 按家族名归入中文 / 英文字体，代码字体只列等宽家族）；`fontPicker` 是设置页与向导共用的字体下拉（搜索、推荐 / 本机字体分组、每项样字、限高滚动），键直接绑定，默认字体别名只在显示时对齐（`canonicalFontKey`，空串不触发下载，不能写回）；`prepareFonts` 是唯一准备入口——缺失的可下载字体按固定目录逐个 `fonts.Store.Ensure`（SHA256/字节校验、可取消、进度经存活窗口 `Window.Update` 回主线程）后 `ui.RegisterFont` 私有注册，完成后只回调 `settle(ok)`。字体应用私有注册，不写入操作系统字体库；本机字体卸载后保留偏好，渲染时回退并提示（`usableFontKey`）。老用户升级后首次开窗由 `preparePreferredFontInBackground` 静默补齐已有偏好引用的缺失字体，失败只记日志。向导与设置页共用选项与准备流程；失败重试、返回修改或换选会作废旧批次（generation 门禁）。 |
| `internal/fonts` | 纯逻辑字体资源模块（无 MyGo 依赖）：固定远程目录（LXGW WenKai Lite / JetBrains Mono / Fira Code 5.2，均为官方 GitHub 固定 commit 的原始 TTF，OFL-1.1，见 `catalog.go`）；`Store.Ensure` 先验证缓存（`<应用数据>/fonts/<资源ID>.ttf`，长度+SHA256），命中不触网，缺失才下载：HTTP 200、严格等长、临时文件同目录随机名 0600、Sync+Rename 原子发布，失败/取消不留半截文件；`LocalKey`/`LocalFamily` 校验 `local:` 键防字体栈注入；`ClassifyFamily`（script.go）按家族名推断中文 / 西文 / 其他文字 / 符号，供字体下拉归类。Dank Mono 等商业字体不在目录，仅本机已装时可选。 |
| `internal/app/testdata/LXGWWenKaiLite-Regular.ttf` | 手写字体栈的无头测试夹具（原 `resources/` 内置字体迁入），仅 `go test` 读取，不随包分发；`font_hand_test.go` 用它验证注册路径与字体栈安全回退。 |
| `resources/icon.png` / `resources/icon.svg` | 「墨 Z」应用图标：米色纸上一笔手写 Z 加朱红句点（1024×1024 PNG 与 SVG 源文件），MyGo 构建时生成 macOS ICNS 与 Windows ICO。设计原型保留在 `docs/demo/app-icon/`，不参与构建。 |

## 线程模型（MyGo 硬约束，违反会在运行期出错或被 `go tool mygo vet` 拦下）

- **app 状态只在主线程改**。`view` 每帧在主线程读状态构建 UI；菜单/托盘/热键回调经
  `withWindow` 回主线程改状态并触发重绘；后台完成回调只经原子发布的活窗口 `Window.Update` 投递，窗口关闭后直接丢弃。
- **后台写盘与提醒解析只接触不可变快照**。提醒主线程复制标签 ID/标题/文本，后台按文本缓存解析块并计算投影；笔记经 `snapshotFile`、偏好经 `prefs.Store.TakeSnapshot`，
  均在主线程拍值拷贝；后台定时器只写快照，绝不读写 `app` 的可变字段；
  协调状态在 `notes.Store` / `prefs.Store` 内部 mutex 中，偏好另用修订号防旧覆盖新。
- **每帧构建的 UI 元素（`ui.*` 控件）不存入 `app`**。`app` 里只保留窗口/菜单这类桌面集成句柄
  （`*mygo.Window`、`*mygo.Menu`、`*mygo.MenuItem`），普通跨帧状态一律存普通字段。
- **跨线程刷新用 `Window.Update`**。`runOnMain` 只读取原子发布的活窗口并投递；窗口不存在时不执行回调，绝不退回后台 goroutine 直接改 `app` 状态。
- 退出流程（`finishQuit` → `shutdownForQuit`，主线程）：停保存定时器 → 置 `stopped` → 最终快照保存并等它完成。
  笔记与偏好均成功后才退出（更新器的重启也经过此流程）；任一保存失败则取消退出、恢复笔记 Store 接收保存，
  重开主窗口显示错误，恢复提醒调度与墙钟巡检，允许继续编辑与重试。

## 持久化生命周期

数据文件：`<数据目录>/zakki.json`（目录见下节）。

### 保存（唯一权威文件：`storage.dataFile` 指向的 `zakki.json`）

1. **固定截止**：首笔未保存修改起 `storage.saveIntervalMs`（默认 2 秒）后开始写盘；
   期间继续输入只替换待写快照，不推迟截止时间；无修改不重复写。失败自动按同一间隔重试。
2. **持久写入**：`notes.Store.writeFile` 先写同目录随机临时文件，完整写入并 `Sync` / `Close` 后 rename，再同步父目录（Windows 不执行目录同步）；替换已完成但目录同步失败时仍更新磁盘指纹并保留保存错误，重试会重新写入和同步，成功前不报已保存。写盘前 `verifyDisk`
   核对磁盘字节指纹，文件被外部改动/移动/删除（`ErrExternalChange`）或处于写保护
   （`ErrSaveBlocked`）时拒绝写入并上报，绝不用内存内容盲写覆盖。
3. **最终保存**：退出（含更新重启）经 `Shutdown` 等待在途写入并写最终快照；失败阻止退出、
   保留编辑会话，允许修复后重试。停止后新到的 `SaveNow` 返回 `ErrStopped`；菜单保存将退出期间的迟到请求视为无操作，普通同步调用仍能获知未写入。
4. **迁址**：更换保存位置走 `Relocate`——拒绝覆盖已有目标 → 写入并校验候选文件 → 偏好持久化
   成功后才切换权威路径 → 删除旧文件；提交前失败保留原路径与原件。`RelocateResult.Committed` 明确区分切换是否完成，提交后的旧文件清理或偏好目录同步失败仍采用新路径并报告警告；候选文件优先以硬链接排他发布，不支持硬链接时以 `O_CREATE|O_EXCL` 排他写入、同步并回读验证；目录先同步，再允许偏好指向它。

### Git（文件位于仓库内且用户开启后，直接跟踪该文件）

`git_sync.go` / `git_schedule.go`：后台单 worker，提交与推送各自按间隔调度（默认 1 / 10 分钟，
失败也计一次尝试）；提交只 `add`/`commit` 当前笔记文件的精确路径（支持仓库子目录与 worktree），
不碰仓库其他文件或暂存内容；推送 `git push` 的是分支历史，无法限定单文件，推荐独立笔记仓库；
调系统 git CLI（超时 1 分钟），macOS 垫片探测、非交互环境变量与「不做 pull/rebase/force push」
沿用原实现；配置失效、写保护或 `Close` 时按代号作废在途任务并取消上下文；非快进等远端拒绝推送报告 `ErrPushRejected` 并提示手动同步、解决冲突后再推送；`GitStatus` 发布
仓库根、最近提交/推送时间与错误，供「保存与同步」页展示。

### 异常处理（无备份副本）

- 文件不存在且未配置过 `storage.dataFile`：首次运行，写入欢迎文本。
- 解析失败 / 读取失败 / 版本过高 / 标签超过 9 个：进入写保护，**保留原件、拒绝写入、不生成 `.pending` /
  `.corrupt` 副本**；用户修复文件后点「重新读取文件」（`reloadNotes`）显式恢复。
- 已配置的自定义路径在重启时丢失：同样报错并写保护，不当作首启重建。
- 外部程序修改了文件：保存前指纹核对失败，拒绝覆盖；显式重读有效内容后继续。

### hideOnEscape 缺键默认 true

设置里其他项默认值都是 false/空串，唯独 `hideOnEscape` 默认 **true**。JSON 的 bool 分不清
「缺键」与显式 `false`，故 `hasHideOnEscapeKey` 用 `map[string]json.RawMessage` 探测键是否存在：
缺键（含 settings 整体缺失）按 true 处理，显式 false 保留。

## 数据文件格式（`savedFile`）

```json
{
  "version": 3,
  "active": 0,
  "tabs": [{ "id": 1, "text": "…", "color": "#…" }],
  "settings": {
    "themeSource": "system",
    "alwaysOnTop": false,
    "hideOnEscape": true,
    "source": false,
    "tabMotionOff": false,
    "tabDotShape": "dot"
  }
}
```

| 字段 | 类型 | 含义 |
|---|---|---|
| `version` | int | 数据版本，当前 `currentDataVersion = 3`；0/1 视为固定 7 点格式；更高拒载不覆盖 |
| `active` | int | 当前激活的标签下标（载入时按标签数 clamp） |
| `tabs` | [{id,name,text,color}] | 动态标签（1–9 个）：稳定 ID、可选名称、markdown 纯文本、颜色（空/非法回退调色板）；v1 的 `dots` 字段仅为读取保留，新保存不写 |
| `settings.themeSource` | string | `system` \| `light` \| `dark`，空串按 `system` |
| `settings.markdownTheme` | string | 空 / `sonnet` 为 Sonnet，`note` 为便签（版本 2 文件里的更名前旧 ID 随载入迁移到新 ID） |
| `settings.bodyFont` / `settings.monoFont` | string | 正文 / 代码字体预设，空为主题默认 |
| `settings.bodySize` / `settings.lineHeight` | float32 | 正文字号 / 行高，0 为主题默认 |
| `settings.alwaysOnTop` | bool | 窗口置顶 |
| `settings.hideOnEscape` | bool | 按 Esc 隐藏窗口，**缺键默认 true**（见上） |
| `settings.source` | bool | 旧字段仅读取；源码/预览切换是会话状态，新保存不再写入 |
| `settings.preview` | bool | 旧版字段，仅读取时并入 `source`，不再写出新值 |

## 偏好生命周期

`Run` 先调用 `resolveDataDir()` 解析数据目录；失败则不构造 Store，在 `WhenReady` 显示原生错误后退出。
随后 `loadPreferences` 读取 `preferences.json`：存在则严格解码（含旧版本字段升级）；缺失时直接
写入默认值——全新安装不导入旧 config 文件。然后 `openConfiguredNotes` 按 `storage.dataFile`
打开唯一笔记文件（空串 = 应用数据目录 `zakki.json`）并 `load` 数据；配置路径丢失时报错并写保护。
运行期所有偏好变更走 `updatePreferences` 统一入口：校验 → 应用运行时 → 同步菜单/窗口/热键 →
防抖自动保存；磁盘写经 `prefs.Store` 串行化、tmp/rename 原子替换，修订号防旧快照覆盖新值。
`preferences.json` 损坏或版本超前时进入待恢复状态：不覆盖原文件，设置页提供「另存原件并恢复默认」；恢复成功后当前会话立即按恢复后的路径重新载入笔记，只有载入成功才解除写保护。
偏好版本当前为 **13**：版本 13 新增正文区左右/上下内边距覆盖；版本 12 由 `storage` 与 `git` 取代 `backup` 与防抖毫秒（见 `internal/prefs` 行）；
更早版本的键升级语义见 configuration.md。退出流程（`shutdownForQuit`）在笔记数据最终保存后等待
偏好最终保存，任一失败阻止退出或更新重启。完整契约见 [偏好与设置](configuration.md)；
MyGo 更新器另外管理 `updater.json`，不改变应用偏好的 JSON 契约。

## 测试接缝

- **路径注入**：生产 `Run` 只调用一次 `resolveDataDir()`；解析结果传给 `mygo.App.SetPath(PathUserData, dir)` 与 `newAppAt(dir)`。系统数据根目录由 `mygoAppDataBase` 解析（macOS 为 `~/Library/Application Support/`，Windows 为 `%APPDATA%`）。
- **共享内核测试**：`core` 嵌套 module 的用例（令牌簇、`^` 规则、实例展开）经 `go -C core` 执行，已纳入 `make test` / `make race`；架构检查在 `make arch-check` 中校验 core 的零依赖叶子身份。
- **时钟注入**：`app.now func() time.Time`，生产 `time.Now`，测试给假时钟。
  创建后 `now` 只读，后台可安全读取。
- **`newTestApp(t)`**（`main_test.go`）：注入 `t.TempDir()` 与固定时钟（2024-06-01），
  **绝不写真实用户目录**。保存、写保护与 Git 调度在 `internal/notes`；`persistence_test.go` 覆盖载入映射与保存语义。
- **原生生命周期回归**：`ZAKKI_NATIVE_TEST=1 MYGO_LOCALE=zh-Hans go test ./internal/app -run '^TestNativeDockReopen$' -count=1 -v` 在 macOS 图形会话中构建独立 bundle、使用临时数据目录，验证关闭重建、隐藏复用、最小化恢复和可见时不重复开窗。默认 `MYGO_GPU=0` 隔离驱动初始化；显式 `MYGO_GPU=1` 可复测 Metal 路径，耗时与稳定性受驱动影响。
- **`ui.NewTester(a.view, w, h)`**：无窗口跑视图（构建/断言文本、模拟按键），
  `main_test.go`/`tab_style_test.go`/`live_boundary_test.go`/`live_focus_test.go` 用它。
- **偏好测试**：`internal/prefs` 的测试覆盖默认值、严格解码（已移除的旧 backup 字段在兼容版本中摘除）、损坏与超前版本恢复、写失败报告、修订号防回写；笔记版本迁移表在 `internal/notes`；
  `menu_commands_test.go` / `internal/markdown` 的格式与行内边界测试 / `view_theme_test.go`
  分别覆盖加速器与冲突校验、纯格式变换（含 CJK/emoji/光标映射）与全主题对比度；
  `menu_contract_test.go` 验证新菜单结构、设置窗口四页（`ui.NewTester`）与改键流程。
- 测试命令见 `Makefile`：`make test`（`CGO_ENABLED=0 go test ./... -count=1`）、
  `make race`、`make vet`（含 `go tool mygo vet .` 的 UI 生命周期检查）。
