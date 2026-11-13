# MyGo v0.9.0 升级后的新能力接入评估

## 背景与范围

`4cba572` 把 MyGo 从 v0.5.1 升到 v0.9.0（4 个 vendor 补丁按新上游结构重放），
`942b6e2` 修好设置页语言行。本报告逐项评估升级计划里列出的五项功能优化
（B1–B5）在 Zakki 现有架构下有没有落地点：能接入的按项提交，不能接入的写清
客观条件，避免下次升级重复调研。

## 结论

| 项 | 上游能力 | 结论 | 处理 |
|---|---|---|---|
| B1 | `ui/richtext.Theme.ParagraphSpacing` / `FirstLineIndent` | 不适用：Zakki 不用 `ui/richtext`，自建预览没有首行缩进的渲染路径；段间距已由 `markdownLayout.blockGap` 提供 | 无代码变更 |
| B2 | 更新窗口跟随 `mygo.App.Locale()`；`updater.OnInstalled`；失败重试 | 语言跟随可接入且有实际收益；`OnInstalled` 无落地点 | `f967f0f` |
| B3 | 原生 Edit 菜单按焦点启用 role 项 | 已是最小实现：两条路由都必要，窗口兜底表无冗余 | 无代码变更 |
| B4 | 窗口内菜单的 `MenuItem.Danger()` | 可接入：标签右键菜单的删除项 | `8a6eebd` |
| B5 | `Span.DottedUnderline`（链接 hover 点状下划线） | 不适用：预览里的链接是静态 span，没有 hover 状态 | 无代码变更 |

## B1 中文排版：段间距与首行缩进

上游把这两个参数放在 `ui/richtext` 的 `Theme`（整篇富文本编辑器组件）：
`ParagraphSpacing` 是正文段落之间的 em 间距，`FirstLineIndent` 是正文段落首行
缩进，默认值都是 0（不开）。Markdown 视图用的 `ui/markdown.Theme` 只有块间距
`BlockSpacing` 与紧凑列表的 `ItemSpacing`，没有段落级间距和首行缩进。

Zakki 两个包都没用：实时预览是自己按块构建的（`internal/app/live_*.go`），
排版参数在 `markdownLayout` 里。

- 段间距：已经存在。`markdownLayout.blockGap`（sonnet 12px @ 16px 字号 = 0.75em）
  由 `listGapAfter` 画在块尾 padding：源码空行分隔块两侧各计一个间距，普通段落
  之间合计 24px（≈1.5em）；同一列表的相邻项收紧到 1/4，避免列表节距接近双倍行距。
  这已经覆盖了上游 0.5em 量级的默认段间距，再加一层会与 Markdown 空行语义重复。
- 首行缩进：没有渲染路径。`ui.RichText` / `ui.Text` 元素不接受缩进参数；
  `text.Params.Indent` 只经 `ui.ShapeRichTextWith(ui.TextShape{Indent})` 暴露在
  排版测量层（给应用自绘的渲染器用），元素渲染路径不会传它。

  在预览层实现有三条路，都不划算：段首插两个全角空格会改变可见文本，而点击定位、
  光标、选区、查找命中全按 rune 偏移与源区间换算（`liveProjection.units`），得再补
  一套偏移补偿；整块左右留白是悬挂缩进，不是首行缩进；用 `ui.ShapeRichTextWith`
  排版、`Painter.TextLayout` 自绘虽然能拿到 `Indent`，却要把预览块的测量、绘制与点击
  命中全换成自绘路径（搜索高亮、选区底色、时间对象都在原生控件里）。而且三条路都
  无法在聚焦时保持——焦点块回到原生输入框后读不到这个参数，点击段落首行的瞬间文字
  会横向跳动。

按升级计划的降级条件（首行缩进若在主题层无法排除即只保留段间距），这里不实现首行
缩进，也不引入偏好项。

## B2 更新器

**语言跟随（已接入）** `Options.Language` 留空时，插件每次开窗取
`mygo.App.Locale()`（`plugins/updater/updater.go` 的 `text()`，按语言缓存）。Zakki
的 `App.Locale()` 只会是 `en` 或 `zh-Hans`（i18n 目录就是应用书写的语言），插件
内置这两种译文，所以留空即可让中英文界面各自看到同语言的发布说明与按钮；原先写死
`zh-Hans` 会让英文界面用户看到中文更新窗口。`updates_test.go` 已锁定“留空 = 跟随”。

**`OnInstalled`（不接入）** 插件在安装完成、重启前回调。Zakki 的重启走退出钩子
`finishQuit`：保存笔记与偏好失败会 `PreventDefault` 取消重启并提示，已有
`TestQuitSavesBeforeUpdateRelaunch` 与 `TestQuitFailureCancelsUpdateRelaunchAndAllowsRetry`
覆盖，没有别的收尾动作需要挂在安装回调上。

**失败重试（语义不变）** 重试只作用于自动检查的调度（`failed.Add(min(interval, time.Hour))`），
Zakki 保持 `DisableAutomaticChecks: true`，只从菜单手动检查；检查失败的提示与再次
检查由原生窗口负责。

**发布说明** 渲染在插件原生窗口内（`plugins/updater/native`），应用侧不需要接线。

## B3 Edit 菜单与快捷键兜底

MyGo v0.9 的 Edit role 项只在焦点（或对应快捷键）能消费时启用
（`docs/ui/input.md`：*The Edit menu enables a role's item only while the focus, or such a shortcut, would take it*）。

Zakki 的两条路由都不能删：

- `menu_edit.go` 在文档聚焦时用 Click + `runEditCommand` 接管撤销/重做/剪切/复制/
  粘贴/全选，是为了覆盖“预览态没有控件焦点、但文档级撤销仍然有效”的情况。若改回
  role，预览态这些项会被原生启用规则置灰，文档级撤销也就没了入口。
- `shortcut_fallback.go` 早已把编辑角色排除在窗口兜底之外（注释写明由引擎内建），
  没有可删的重复注册；删除反而会让编辑命令在引擎处理之后再触发一次。

`editorContextMenu` 与菜单项共用 `runEditCommand`，同样保留。该项无代码变更。

## B4 菜单项的 Danger

`ui.MenuItem.Danger()` 只影响窗口内绘制的菜单（`ui.Menu` 的 ContextMenu、`PopoverMenu`）；
系统菜单 `mygo.MenuItem` 没有这个字段，应用菜单栏里的删除项无法标注。Zakki 只有两个
窗口内菜单：

- 标签右键菜单：删除标签是唯一破坏性项，已标注 Danger（`8a6eebd`）。文字取主题
  `Danger` 色，悬停时在高亮上取可读色；动作、禁用条件、稳定 ID 重定位都不变，
  可识别性仍由独立分组与标签文字承担（颜色不能替代无障碍信息）。
- 编辑器右键菜单：撤销/重做/剪切/复制/粘贴/全选，没有破坏性项。

## B5 链接 hover 点状下划线

`ui.Span` 有 `DottedUnderline`（`ui.TextRange` 没有，区间样式补丁也没加），但它是静态
样式；上游在 `ui.Link` 上用它表达悬停。Zakki 预览里的链接是 `ui.RichText` 的 span，
不是可交互元素：

- 没有 hover 状态——非焦点块整体只响应点击，不逐 span 跟踪指针；
- 链接本身不点击打开浏览器，`href` 只用于 ⌘K 链接浮层的回填。

因此“hover 点状下划线”没有挂载点；把常态下划线换成点状只会降低可读性，且焦点块
（`ui.TextRange` 路径）只能保持实线，两种状态的链接样式会不一致，不做。

## 验证

- 每个提交前 `make check` 全绿：fmt-check + vet（含 `go tool mygo vet`）+ test +
  race + arch-check + vendor-check；vendor 的 `ui` 包回归测试在 `make test` / `make race`
  里显式执行。
- 启动冒烟（本机 macOS，`make build` 的 Zakki.app）：载入真实数据后标签、列表预览、
  手账纸面与状态栏渲染正常；应用菜单里的「检查更新…」弹出原生更新窗口，中文界面显示
  「软件更新 / 已是最新版本」。
- 语言跟随另在隔离的临时 HOME 里验证（不碰用户的偏好与笔记）：界面语言设为 `en` 后
  同一入口弹出的窗口为 `Software Update / You're up to date!`，确认 `Options.Language`
  留空确实是按 `App.Locale()` 解析，不是写死的中文。
- 自动获得的收益（VS Code 风格滚动条、0.6/0.8 性能提升、列表低开销、会话结束的
  退出钩子、编辑器上方滚动穿透修复）随升级生效，未单独提交；非活动窗口 accent、
  英文 Windows 上的中文字体统一仍取上游默认值，留在下次 Windows 真机接触时顺带确认。
- 尚未覆盖的手动冒烟项（需人工操作）：编辑器样式（底色/波浪线/等宽/斜体）、
  ⌘⌥←/→ 切标签、快捷键录制、查找栏、表格编辑、时间对象与日期时间弹层、托盘、
  设置各页内交互、退出保存。
