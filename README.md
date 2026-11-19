# Zakki — 原生 Go 的 Markdown 速记便签

Zakki 是一个 macOS / Windows 桌面速记应用：多个彩色标签各存一段 Markdown，
Typora 式所见即所得编辑，纯 [MyGo](https://github.com/egoist/mygo) 原生 Go UI
实现——无 HTML/JS/webview，单二进制，GPU 绘制，界面语言内置英文与简体中文
（默认跟随系统）。

## 功能

- **彩色标签速记**：默认 7 个标签（可 1–9 个，与 `⌘1`–`⌘9` 一一对应）。未命名时每枚一样大，当前只是颜色更深；
  有名字时，名字写在当前标签的色条里；拖动圆点即可调整顺序。只有一个标签时顶栏保持干净，只留关闭与置顶。
- **所见即所得编辑**：焦点块保留标题/段落/列表/引用排版，Markdown 标记只留在
  内部源文本；表格带悬浮工具栏（调行列、对齐、删除），待办可直接勾选；
  `⌘⇧P` 随时切全文源码模式。
- **代码语法着色**：围栏代码块按语言轻量着色（Go / JS / TS / Python / Shell / JSON /
  YAML / TOML / Rust / C / Java 家族），实时预览与源码模式共用；外观设置可选
  柔和 / 明快 / 跟随文本。
- **长文档流畅**：预览块列表只构建可见块，几千块的笔记滚动与按键依然轻快。
- **单一笔记文件 + 自动保存**：所有标签存进一个 `zakki.json`，不再生成历史副本；
  有修改按固定间隔（默认 2 秒）自动保存，持续输入不推迟，`⌘S` 立即保存，退出前最终保存。
  文件放进 git 仓库可开自动提交与推送（只提交这一个文件）。
- **界面语言**：英文 / 简体中文，跟随系统，可在首启向导、设置或菜单随时切换。
- **顶栏个性化**：切换动画可关闭；标签标记可选圆点 / 圆环。
- **原生体验**：全局热键呼出、窗口置顶、关窗驻留托盘、单实例、
  深浅色跟随系统、全套快捷键可录键重绑定。

## 安装

从 [GitHub Releases](https://github.com/gomeen/Zakki/releases/latest) 下载：

- **macOS**：通用 DMG（Intel 与 Apple Silicon），拖入「应用程序」。当前为
  ad-hoc 签名（无 Developer ID 与公证），首次打开需在「系统设置 → 隐私与
  安全性」里允许。
- **Windows 10/11 x64**：`zakki-setup-<版本>-amd64.exe`，按用户安装、无需管理员；
  未做 Authenticode 签名，SmartScreen 可能提示，确认来源后运行。

应用内 **Zakki → 检查更新…** 可手动检查并安装签名更新（默认不后台检查）。

## 快捷键速览

macOS 用左列符号；Windows 把 ⌘ 换成 Ctrl、⌥ 换成 Alt、⇧ 换成 Shift。
两平台的唯一差异是标题键：macOS 的标题用 ⌃（次级修饰键），Windows 用 Alt。

| 按键（macOS） | 作用 |
|---|---|
| `⌘1`–`⌘9` | 切换标签（Windows：`Ctrl+1`–`Ctrl+9`） |
| `⌘T` / `⌘R` | 新建 / 重命名标签 |
| `⌘⌥←` / `⌘⌥→` | 上一个 / 下一个标签 |
| `⌘B` / `⌘I` / `⌘K` | 加粗 / 斜体 / 链接 |
| `⌃1`–`⌃6` / `⌃0` | 标题 1–6 / 正文（Windows：`Alt+1`–`Alt+6` / `Alt+0`） |
| `⌘⇧8` / `⌘⇧7` / `⌘⇧9` | 无序 / 有序 / 任务列表 |
| `⌘⇧Q` / `⌘⌥C` | 引用 / 代码块 |
| `⌘F` / `⌘G` / `⇧⌘G` | 查找 / 下一个 / 上一个（查找框内 `Enter` / `⇧Enter`） |
| `⌥⌘F` / `⇧⌘F` | 查找和替换 / 在所有标签中查找 |
| `⌘⇧P` | 源码模式 |
| `⌘S` | 立即保存 |
| `⌘⇧C` | 复制标签全文（文件菜单 / 标签右键还可「导出为 Markdown…」） |
| `⌘⇧空格` | 全局显示/隐藏（可改键） |
| `⌥⌘T` | 窗口置顶 |
| `⌘=` / `⌘-` / `⌘0` | 放大 / 缩小 / 默认正文字号 |
| `⌘,` | 设置 |

Markdown 命令与全局唤起都可在 **设置 → 快捷键** 里录制改键。

## 数据与保存

- 数据目录：macOS `~/Library/Application Support/Zakki/`，
  Windows `%APPDATA%\Zakki\`。
- 笔记文件 `zakki.json`：全部标签文本，tmp+rename 原子写入，应用只维护这一个文件；
  偏好文件 `preferences.json` 是所有设置的唯一来源。
- 首启向导可选择保存位置（任意文件夹，含 git 仓库）；之后「设置 → 保存与同步」
  可随时更换（拒绝覆盖已有文件，失败保留原件）。
- 文件损坏或被外部改动时保留原件、暂停写入，提供「重新读取文件」显式恢复；
  退出前最终保存失败会阻止退出并说明原因。

## 开发

```sh
go tool mygo dev      # 开发（热重载）
make test             # 全量测试（含 MyGo vendor 补丁包）
make check            # gofmt + vet（含 mygo vet）+ test + race + arch 检查（CI 同款）
make build            # macOS 通用 .app / .dmg
make build-windows    # Windows x64 .exe
```

界面文案在 `locales/*.json`（ICU MessageFormat，ARB 格式）；
`go tool mygo generate` 生成 `locales/catalog_gen.go`，键位一致性有测试保障。
工程标准与模块职责见 [docs/architecture.md](docs/architecture.md)。

## 性能

编辑与预览采用块级虚拟列表，长文档帧成本主要随可见块数增长。历史测量与未解决瓶颈见 [docs/reports/](docs/reports/)。

## 许可

[MIT](LICENSE)。内嵌资源各自遵循其许可：Lucide 图标（ISC）。霞鹜文楷 Lite、JetBrains Mono
与 Fira Code 字体（均为 OFL）不随安装包分发，应用内在首次选择时从各自官方 GitHub 仓库
下载（见 `internal/fonts/catalog.go`），许可文本保留在 `resources/OFL-*.txt`。
