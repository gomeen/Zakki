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

## 截图（M2-5 实时样式）

浅色 / 深色 / 横屏（release 资产 `m2-5-pr-assets`）：

- [m2-5-light.png](https://github.com/gomeen/Zakki/releases/download/m2-5-pr-assets/m2-5-light.png)
- [m2-5-dark.png](https://github.com/gomeen/Zakki/releases/download/m2-5-pr-assets/m2-5-dark.png)
- [m2-5-landscape.png](https://github.com/gomeen/Zakki/releases/download/m2-5-pr-assets/m2-5-landscape.png)

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
