# 2026-10-29 移动端 M2 编辑器验证记录

对应 [docs/mobile-spec.md](../mobile-spec.md) §7 里程碑 M2（实施票
[#26](https://github.com/gomeen/Zakki/issues/26)）：双模式编辑器、令牌簇往返、写回管道、
源码模式与快捷栏、最小主题集、标签管理绑定扩展、系统分享导出与实时 Markdown 样式渲染。
五个子票全部合入 `main`，自动化验证完成；长中文重度编辑等人工验收项**尚未执行**，
边界见文末。涉及真实用户数据的验收（#26）须使用副本，副本不进仓库。

## 子票与 PR 对照

| 子票 | PR | 内容 | 测试变化 |
|---|---|---|---|
| [#27](https://github.com/gomeen/Zakki/issues/27) | [#31](https://github.com/gomeen/Zakki/pull/31) | M2-1 编辑器内核：实时渲染、令牌簇收簇往返（U+FFFC 单字符缓冲、磁盘保留原始 Markdown）、raw 写回 | 40 条 XCTest 起步 |
| [#29](https://github.com/gomeen/Zakki/issues/29) | [#32](https://github.com/gomeen/Zakki/pull/32) | M2-3 最小主题集：明暗语义色 12 字段、sonnet 排版、标签色板与对比度收敛逐位移植 | ThemeSyncTests 防漂移（主题值与桌面源逐位对照） |
| [#28](https://github.com/gomeen/Zakki/issues/28) | [#33](https://github.com/gomeen/Zakki/pull/33) | M2-2 源码模式与 Markdown 快捷栏 | +24 条 → 64 |
| [#30](https://github.com/gomeen/Zakki/issues/30) | [#34](https://github.com/gomeen/Zakki/pull/34) | M2-4 标签管理与导出：`AddNoteTab`/`DeleteNoteTab`/`ReorderNoteTabs` 绑定扩展、系统分享导出 | Go 契约 +13 用例，Swift +14 → 91 |
| [#35](https://github.com/gomeen/Zakki/issues/35) | [#37](https://github.com/gomeen/Zakki/pull/37) | M2-5 实时 Markdown 样式渲染：块级六类 + 行内四类 + 标记调淡，只加属性不改字符 | +35 条 → **126 全绿** |
| [#39](https://github.com/gomeen/Zakki/issues/39) | 本 PR | M2-6 实时预览与桌面一致（Typora 式标记隐藏，取代 M2-5 形态） | 126 → **177 全绿** |

## 自动化验证

- iOS XCTest 126 条，本机（iPhone 17 Pro 模拟器，iOS 26.5）全量通过；mobile.yml 在 PR #37 起
  常态运行同套测试。
- 性能预算：2000 行富文本整篇重上（生产入口，热）实测 **0.19s**（Debug，iPhone 17 Pro 模拟器）；
  CI 预算断言放宽到 0.4s，即实测 2× 余量，兜住复杂度回退。
- Go 侧：`mobile/binding` 契约测试覆盖绑定扩展（增删、重排、错误码），core/model
  未知字段往返与写保护契约延续 M1。

## 两次数据丢失级回归的经过与修复

1. **缓冲↔raw 映射错位吞文本**（M2-2 期间发现，`b22b3f7` 引入面）：
   `EditorDocument` 缓冲↔raw 通用映射对组件片元缺端点语义——组件后位置应映射到簇尾，
   却落回簇首；同时命中替换区间的越区分支用线性换算，而非「变异后/重扫前」几何快照。
   行内含组件时编辑产生错误替换区间，视图钳制写入后**吞掉组件之后的文本**；
   快捷栏前缀动作（在行首插入记号）恰好触发。修复 + 3 条文档级回归（随 PR #33）。
2. **`rescanLines` 相对坐标违背自身契约**（PR #37 期间发现，M2-1 潜伏缺陷）：
   命中区间使用行内相对坐标，违背其注释声明的「新 raw 绝对坐标」契约——
   非首行收簇会吞掉行首至令牌之间的全部文本。修复：命中区间加 `line.rawStart`；
   回归 `testTypingCollapseOnLaterLineCollapsesOnlyTheCluster`（随 PR #37）。

两条都属于「自动化未覆盖到才漏到后续票」的场景，修复均带文档级/多行场景回归，
防止同类收簇写坏再次回归。

## 桌面配套

[#24](https://github.com/gomeen/Zakki/issues/24)（[#36](https://github.com/gomeen/Zakki/pull/36)）
桌面读写切换到 `core/model` 的 Decode/Encode：顶层、settings、tab 三层未知字段跨端往返，
无未知字段时输出与旧 `json.MarshalIndent` 逐字节相同。移动端 M2 导出 → 桌面打开 →
桌面再保存**未知字段不丢**的全链路自此成立（人工回读步骤见 #26 验收指引）。

## 截图（M2-5 实时样式；形态已被 M2-6 取代，历史保留）

浅色 / 深色 / 横屏（release 资产 `m2-5-pr-assets`）：

- [m2-5-light.png](https://github.com/gomeen/Zakki/releases/download/m2-5-pr-assets/m2-5-light.png)
- [m2-5-dark.png](https://github.com/gomeen/Zakki/releases/download/m2-5-pr-assets/m2-5-dark.png)
- [m2-5-landscape.png](https://github.com/gomeen/Zakki/releases/download/m2-5-pr-assets/m2-5-landscape.png)

## M2-6 实时预览与桌面一致（Typora 式标记隐藏）

实施票 [#39](https://github.com/gomeen/Zakki/issues/39)（用户裁决：与 PC 完全一致，
M2 内补票，#26 验收前置）。**M2-5 的「标记调淡」形态被本票取代**：标记不再以
textMuted 灰显示，而是全程不可见——非焦点块按桌面 livePane 渲染，焦点块只编辑
可见文字（本节替换上节截图作为实时样式验收基线；M2-5 记录保留作历史）。

### 桌面行为 → 移动端实现对照（要点）

| 桌面（internal/app） | 移动端（mobile/ios/Zakki/Editor） |
|---|---|
| `liveBlocks`/`parseLines`：围栏成块、空行独立、标题/引用/分隔线/列表/待办单行块、连续普通行并为段落 | `LiveBlocks.split` 逐条移植（UTF-16 坐标）；表格行不特判并入段落（移动端无表格块，M2-5 同） |
| `liveProjection`：块级/行内标记剥离，可见↔源偏移映射；不完整语法保持字面，补全后重投影即成型 | `LiveProjection`（units/styles + `replacing`/`fragment`），「输入中即时成型」有专项测试 |
| `live_edit`/`live_input`：块尾回车拆块（段落 `

`、列表续行前缀、空列表退出）、块首退格并块（空行段收敛、裸标记退出、代码块不并）、越界方向键跨块 | `LiveDocEditing` 纯函数 + 容器触发，桌面 `TestLiveBackspaceBoundary` 等边界用例全量对齐 |
| `live_timeprojection`：时间令牌在可见文字中收成单 U+FFFC，选区压上扩到整组件，组件后输入自动补空格 | `withTimeObject` 移植；令牌识别只走 core 绑定（FindTimeToken + MarkdownProtection），剪贴板 copy/cut/paste 展开整簇 |
| `live_history`：文档级撤销（连续键入合并、粘贴/IME/命令独立、围栏逐键与补闭栏合并） | 容器级区间片段事务栈（容量 200），块视图撤销路由到容器；undo/redo 焦点与光标随事务恢复 |
| `livePane`/`live_preview_cache`：虚拟化渲染非焦点块，焦点块保留排版结构 | UIScrollView + 块高表（离线排版测量缓存 + `listGapAfter` 尾距），只挂载视口附近块；焦点块 = BlockEditorView（标题字号/引用竖条次要色/列表悬挂/任务 ☐☑ 前缀不可点/代码卡片/围栏行隐藏） |

### 关键取舍（与桌面的已知差异）

- **表格**：移动端无表格块，表格行并入段落按普通文本处理（M2-5 同；桌面表格网格编辑不随本票）。
- **任务勾选框**：仅展示 ☐/☑，不可点（勾选交互属 M3）。
- **代码块**：无语法高亮着色（mono + 灰底卡片）；围栏开闭行隐藏（同桌面卡片化呈现）；键入 ``` 即字面，回车自动补闭栏（桌面 fenceDraft 同款）。
- **词中下划线/链接内标记**：按桌面 `inlineSpans` 口径（无 CommonMark 词边界规则），M2-5 的词边界规则只属源码高亮口径，不随 live 投影。
- **撤销粒度**：键入 + 紧随回车拆块按桌面连续性规则合并为一个事务（一次撤销整体回退）；块视图不再各自持撤销栈。
- **簇内 raw 偏移钳制**：切换到组件后（桌面口径），M2-5 曾钳组件首。

### 测试迁移（126 → 177）

- 新增 `LiveBlockModelTests`（38）：分块规则、投影/映射往返、不完整语法字面与成型时机、
  replace/fragment 包裹保留、时间令牌占位与自动补空格、代理对边界、桌面边界用例（并块/拆块/空行）。
- 新增 `LiveEditorContainerTests`（27）：焦点迁移、raw 逐字保真、容器级撤销/重做（含焦点移动、
  键入-拆块合并、粘贴独立事务）、raw 选区映射、快捷栏源码级动作、围栏草稿、跨块方向键、
  样式不改 raw/撤销栈、2000 行性能预算。
- 新增 `BlockEditorViewTests`（14）：可见缓冲收簇、剪贴板 raw 投影、组字门控、容器协商钩子。
- 迁移 `MarkdownStylerTests`（18）：标记调淤断言改「标记不在可见文字」；非标记样式断言
  等价保留；`SourceModeTests`（10）/`MarkdownToolbarTests`（7）迁到容器/纯文本双路径；
  `EditorDocument` 等无标记语义测试零改动零回归。

### 性能实测（Debug，iPhone 17 Pro 模拟器，iOS 26.5）

- 2000 行混合 Markdown 全量分块 + 逐块投影：**76ms**（预算 250ms）。
- 2000 行装配 + 激活末块 + 首键入：**2.07s**（预算 2.5s；含 1600 块首次测量）。
- 稳态键入结算（写回 + 全量重切 + 重投影，5 次取最差）：**83ms**——与 M2-5 整篇热重上
  190ms 同量级；视口外不布局，滚动与首帧成本只与可见块数相关。

### 截图（M2-6，浅/深/横屏，release 资产 `m2-6-pr-assets`）

- [zakki-m2-6-light.png](https://github.com/gomeen/Zakki/releases/download/m2-6-pr-assets/zakki-m2-6-light.png)
- [zakki-m2-6-dark.png](https://github.com/gomeen/Zakki/releases/download/m2-6-pr-assets/zakki-m2-6-dark.png)
- [zakki-m2-6-landscape.png](https://github.com/gomeen/Zakki/releases/download/m2-6-pr-assets/zakki-m2-6-landscape.png)

内容覆盖标题/粗斜体/行内代码/链接/任务/引用/代码卡片/分隔线/时间令牌胶囊混排；
三张图均无任何 Markdown 标记（重点验收项）。演示 zakki.json 放临时目录，不入库。

### 过程记录

- 用户模拟器实测触发 SIGILL 崩溃（DiagnosticReports 五份，09:05–09:18）：4 份同一栈
  （`handleEnter → applyStructural → recordOp` fencePair 合并对纯插入旧片段按 delta 切片越界，
  两个测试入口同根因），1 份 SIGSEGV 为旧版 `performBatchUpdates` 路径；均已修复并有对应回归。
- 布局层曾用 UICollectionView self-sizing，实测块叠印后改为显式块高表 + 视口挂载
  （桌面 livePane 同构），离线排版测量与实际渲染共用 TextKit 1 管线保证一致。

## 尚未完成的人工验收项

以下项自动化已覆盖对应逻辑，但**真实数据与真机手感的人工验收未做**，即
[#26](https://github.com/gomeen/Zakki/issues/26) 的勾选框清单：

- 导入用户自己 zakki.json 的**副本**到模拟器；
- 长篇中文重度编辑：长文、多组件、中英混排、撤销/剪贴板/IME 组字；
- 实时样式人工过目：标题/粗斜体/任务/引用/代码块/时间组件混排，浅色 + 深色 + 横屏；
- 写保护文件（高版本/超 9 标签/损坏）保持只读；
- 导出 → 桌面打开 → 桌面再保存后未知字段不丢（依赖 #24 / PR #36）；
- 横屏与软键盘、拖拽排序与分享面板手感。

验收通过后人工关闭 #26，随后进入 M3 规划。
