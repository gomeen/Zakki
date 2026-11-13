# MyGo 依赖补丁

本仓库保留 `go.mod` 锁定的 MyGo v0.9.0，并通过 Go 标准 `vendor/` 机制携带最小补丁。
`vendor/` 包含构建所需依赖及其许可证；普通 `go test`、`go build` 和 `go tool mygo` 自动使用它。
不要用 `-mod=mod` 绕过补丁。

MyGo v0.9 的 `cmd/mygo` 新增了 Linux 打包能力（RPM、OpenPGP 签名、压缩），
`go mod vendor` 会把工具依赖一并复制进来，因此 `vendor/` 体积以这些包为主；
它们只服务 `go tool mygo build`，不参与应用二进制。

## 输入区间样式

`mygo-text-range-background.patch` 为 `ui.TextRange` 补齐实时预览实际使用的属性：

- `Background`：粗体荧光底色与行内代码底色。
- `Font`、`Italic`：局部等宽字体与混排斜体。
- `Underline`、`WavyUnderline`、`DecorationColor`：普通链接下划线与手账波浪线。

非零属性独立合并，重叠时各取最后一个有效值；布尔属性与 RichText 一样只开启样式。
字体、斜体与字重进入布局和 shaping key；合并缓存区间时比较完整布局属性。
背景、文字颜色与装饰只参与绘制，改变或拆分这些属性不改变 shaping key。
复用原生输入框的 `spanPaint`：底色 → 选区 → 文字与装饰，换行、字体回退、双向文字、滚动沿用控件的真实几何。
IME 组合文本保持无区间样式，原区间在其两侧拆分／移动，确认或取消后恢复。

应用层在 `internal/app/live_inline_style.go` 统一预览和编辑的样式定义。
标题装饰仍由两态共用同一容器；列表固定内容文字颜色，避免恢复窗口主题时覆盖继承。
应用回归在 `internal/app/view_live_focus_test.go`、`view_sketch_focus_test.go` 与 `mygo_text_background_test.go`，
覆盖像素一致、墨色、换行、光标边界、表格、选区及 IME。
底层合并、布局 key 与组合区间测试在 vendor 的 `ui/textinput_background_test.go` 和 `ui/textinput_styles_test.go`；
`make test` / `make race` 会显式执行这个包（普通 `go test ./...` 不遍历 vendor）。

## 日期与时间面板

`mygo-calendar-spacing.patch` 调整原生日期面板的列宽和行间距，给中文星期和日期格留出稳定的横向与纵向留白：
`ui/date.go` 的 `calendarBox`（列间距与表头列宽）与 `calendarGrid`（日期格宽度）对着 v0.9 的拆分重写。

应用层的时间弹层使用 `internal/app/live_timepopover.go`，保留原生 DateInput/TimeInput，并在应用层增加点击时刻浮出的小时／分钟滚轮面板。

## 行内时间对象

`mygo-textinput-objects.patch` 接在区间样式补丁之后，给 `TextRange` 增加单字符对象的宽高，并复用 MyGo 原有 `Span.ObjectWidth` / `ObjectHeight` 原生排版能力。对象参与 TextArea 段落的测量、换行与光标命中；真实几何供应用绘制时间文案和点击命中，避免另放一个不属于文字流的按钮。

源码到可见文本的映射仍由应用维护：时间源码在可见缓冲中仅占一个 U+FFFC，原生移动、选区、IME 和删除处理这个单字符；应用负责无损回写源码与文档级撤销。预览与聚焦使用同样的对象尺寸和样式，不向磁盘保存占位符。

## 快捷键优先于编辑器导航

`mygo-shortcut-claimed-keys.patch` 调整 `ui/input.go` 的按键路由：已被登记为快捷键
（窗口兑底或焦点链上的命令）的和弦不再交给聚焦的文本编辑器做词间移动／选区扩展，
而是照常进入快捷键分发——菜单等价键在所有原生平台都优先于文本导航。未登记的和弦
（如 Alt+→ 词间移动、Shift+方向键选区）行为不变。

动机：⌘数字切标签后编辑器持有焦点，⌘⌥←/→（上/下一个标签）会被原生编辑器当作
词间移动消费，窗口级兑底（Windows 的实际触发路径）收不到按键；macOS 有菜单
accelerator 在系统层先消费，不受影响。回归测试在 vendor 的 `ui/shortcut_claimed_test.go`，
`make test` / `make race` 会显式执行这个包。

## 刷新与升级

```sh
make vendor       # go mod vendor 后重新应用补丁；冲突时直接失败，需人工适配
make check
```

不要单独执行 `go mod vendor` 后提交，否则会丢失补丁。升级 MyGo 后，先检查上游是否已支持
对应原生区间属性；若接口和行为等价，移除补丁并保留回归测试，再评估是否仍需提交 `vendor/`。
区间样式补丁修改 `ui/textinput.go`、`ui/editor_layout.go` 及同步后的 `ui/api_gen.go` 文档，
另附上述两个底层测试文件；v0.9 把区间到样式的换算拆成 `rangeKey`/`spansOf`/`styleRanges`，
补丁按新结构重放，布局快速路径同样按字体、斜体与对象尺寸判断是否需要 shaping key。
行内对象补丁还涉及 `ui/textarea.go` 的布局缓存、
`ui/element.go` 与 `ui/paint.go` 的绘制回调、`ui/text_layout.go` 的索引说明，
以及新增的 `ui/textobjects.go` 与 `ui/textobjects_test.go`；不批量改第三方格式。
