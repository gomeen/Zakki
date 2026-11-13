# 移动客户端归档：进度、未完成项与恢复路径

> **状态：暂停，不再继续开发（2026-11-11 决策，见 [ADR-0003](adr/0003-pause-mobile-client.md)）。**
> 目标规格仍在 [mobile-spec.md](mobile-spec.md)，代码仍在 `mobile/`，两者都只冻结、不删除。
> 本文回答四个问题：做到哪了、还差什么、当初要做成什么样、将来想捡起来怎么捡。

## 1. 为什么停

- 只有免费 Personal Team 签名：profile 7 天过期、每设备 3 个 App ID、Time Sensitive
  entitlement 不可用（spec §10 D19，M3 出口的 A2 项因此无法验证）。
- 手机上已安装的 app 因签名过期**已经不可用**，重签流程对便签这种高频工具不可接受。
- 剩余工作（§3.1 全部 11 项 + M5）**几乎全在真机验收**，没有付费账号就无法闭环。
  继续写代码只会得到一份自己用不上的实现。

## 2. 已完成（代码事实，非完成声明）

### 2.1 里程碑

| 里程碑 | 交付内容 | 票 | 验证记录 |
|---|---|---|---|
| M1 脚手架 | 同仓 `mobile/binding`（独立 module，`replace => ../../core`，唯一允许 `golang.org/x/mobile`）+ XcodeGen 工程 + `make mobile-framework` + `mobile.yml` CI + 文件选择器导入 `zakki.json`；高版本写保护、未知字段嵌套往返、超 9 标签不丢数据 | [#25](https://github.com/gomeen/Zakki/pull/25) | [2026-10-28](reports/2026-10-28-m1-scaffold-verification.md) |
| M2 编辑器 | SwiftUI 壳 + UIKit 文本栈双模式（实时渲染 / 源码）、令牌簇往返与写回、Markdown 快捷栏、最小主题集与防漂移同步、标签管理绑定、系统分享导出 | [#31](https://github.com/gomeen/Zakki/pull/31)–[#34](https://github.com/gomeen/Zakki/pull/34)、[#37](https://github.com/gomeen/Zakki/pull/37) | [2026-10-29](reports/2026-10-29-m2-editor-verification.md)（126 条 XCTest）|
| M3 iOS 提醒 | 令牌弹层、单次通知重排、完成推进、重复展开、64 槽跨任务按最早 `fireAt` 分配 + 最后槽续排、状态展示（已排定/待同步/待续排/权限受限降级）| [#51](https://github.com/gomeen/Zakki/issues/51) | [2026-11-08](reports/2026-11-08-m3-ios-reminders-verification.md)（53 条新增 XCTest，模拟器 scheduled=63+renewal）|
| M4-1 同步引擎 + GitHub 后端 A | §2.2 判定表四种情况、sha CAS（409/422 重拉重判定）、Device Flow + 刷新 + 撤销、Keychain、`conflicts/` 留档、冲突对话框 | [#52](https://github.com/gomeen/Zakki/issues/52) | [2026-11-08](reports/2026-11-08-m4-1-sync-verification.md)（46 条新增 XCTest + 进程内 GitHub stub 端到端）|
| M4-2 iCloud 云盘后端 B | 文件选择器授权 + security-scoped bookmark、基线=文件身份 + mtime 的写前比对、命名扫描 + NSFileVersion 双通道冲突副本、书签失效重选状态机 | [#53](https://github.com/gomeen/Zakki/issues/53) | [2026-11-08](reports/2026-11-08-m4-2-icloud-backend-verification.md)（35 条新增 XCTest + 3 场景 + 跨容器真实 picker）|
| iCloud 探针（D21 门禁）| capability 核对、picker 授权、占位文件触发下载、冲突副本枚举——**三项全通过**，所以后端 B 没有回落 M5 | — | [2026-11-08](reports/2026-11-08-icloud-sync-probe.md) |

### 2.2 M4-3 桌面同步配套：**属于桌面，不随移动端冻结**

这条容易被归档误伤，单独说明：同步偏好节（prefs v14）、旧 Git 调度器暂停/恢复、
先拉后提交的同步循环、D13 冲突对话框与留档，都是**桌面实现**，已随 0.6.3 发布
（`f361012`，验证记录 [2026-11-08](reports/2026-11-08-m4-3-desktop-sync-verification.md)）。
冻结的是 iOS 侧同一套引擎，桌面侧照常维护。

### 2.3 代码与测试规模

| 位置 | 内容 | 规模 |
|---|---|---|
| `mobile/ios/Zakki` | SwiftUI 壳、编辑器、提醒、同步、设置 | 73 个 Swift 文件 / 约 17340 行 |
| `mobile/ios/ZakkiTests` | 单元与契约测试 | 331 条 XCTest |
| `mobile/ios/ZakkiUITests` | UI 端到端（不进 CI，依赖中文环境与坐标点按）| 10 条 XCTest |
| `mobile/binding` | gomobile 绑定构建模块 | 2 个 Go 文件（`deps.go`、`doc.go`）+ `go.mod` tool directive |
| `.github/workflows/mobile.yml` | 绑定产物 + 工程生成可复现 | 1 条 workflow |

## 3. 未完成

### 3.1 代码已完成、只差真机/人工验证（11 项）

全部留在 [mobile-spec.md](mobile-spec.md) §8 未勾选行。按「恢复时先做什么」排序：

**第一优先（决定整个路线是否成立）**

| 项 | 内容 | 为什么优先 |
|---|---|---|
| C1 | 真机 picker 里 iCloud Drive 节点可见可选 | 失败则 D21 规定后端 B 回落 M5，路线要改 |
| C4 | 占位文件下载轮询（`notDownloaded` → `.current` → 读全文）| `PlaceholderReader` 的占位分支**从未运行过**，只编译通过 |

**其次（提醒可用性）**

| 项 | 内容 |
|---|---|
| A1 | 免费 Personal Team 下 Time Sensitive 降级确认 |
| A2 | 付费账号 + `Zakki.entitlements` 下的 Time Sensitive 免打扰穿越（**暂停的直接原因**）|
| A3 | 到点响矩阵：锁屏 / 后台 / 专注模式 / 睡眠唤醒 / 手动改时 / 时区切换 |
| A4 | 免费账号 7 天重签流程（**暂停的直接原因**）|
| A5–A7 | 同 bundle 覆盖重签 / 覆盖安装 / 卸载重装，三种情况分别核对数据、授权、pending（不得互相推断）|
| A8 | App ID 每设备配额（每 7 天 10 个）|
| A9 | 节电模式下状态条刷新时机 |
| A10 | profile 过期后的检测与提示 |

**再次（同步）**

| 项 | 内容 |
|---|---|
| B1–B3 | 注册 GitHub App → Device Flow 真实连接 → token 真实过期（8h）静默刷新 → GitHub 侧撤销授权后断连重连 |
| B4 | 蜂窝网真机 `api.github.com` 可达性 |
| B5 | 免费签名覆盖安装/重签后 Keychain 凭据存续 |
| B6 | 限流与大文件（>1MB raw 回退）线上行为——目前仅 httptest mock |
| C2–C3 | 就地写回 Mac 侧可见、未授权直读被拒 + 撤销授权后报 `access-denied` |
| C5 | 真实冲突副本双通道检测（`zakki (N).json` / conflicted copy / NSFileVersion）|
| E1–E4 | **桌面 ↔ iPhone 双向联调**（M4 出口最后一环）、双端真实 sha CAS 冲突、判定表四情况复现、旧 Git 调度器暂停恢复 |

**人工手感**（[#26](https://github.com/gomeen/Zakki/issues/26)）

导入自己 `zakki.json` 的副本、长篇中文重度编辑（长文 + 多组件 + 中英混排 + 撤销/剪贴板/IME 组字）、
实时样式过目（标题/粗斜体/任务/引用/代码块/时间组件 × 浅色 × 深色 × 横屏）、
写保护文件保持只读、导出 → 桌面再保存后未知字段不丢、横屏软键盘与拖拽排序手感。

### 3.2 完全未实现

| 范围 | 内容 | 备注 |
|---|---|---|
| M5 Android | Android 工程 / Kotlin + Compose 编辑器 / AAR 构建与加载 / AlarmManager 闹钟（`setAlarmClock`、精确权限、Doze）/ BOOT_COMPLETED 等冷启动重建 / 时区注入 / 双端小组件 | `mobile/` 下无任何 Android 目录，一行代码未写 |
| M5 权限与生命周期 | Android 12+ 精确权限拒绝与降级、API 33+ `USE_EXACT_ALARM`、`POST_NOTIFICATIONS` 拒绝、通知通道关闭、force-stop 恢复 | 同上 |
| M5 分发 | 签名、分发流程、App Group / WidgetKit capability、16KB 页大小 AAR | 同上 |
| M5 云盘补齐 | OneDrive 国行可达性、iCloud 与第三方 provider 差异、NSFileVersion 恢复入口、第二台桌面加入已有文件（spec R8）| 同上 |
| #57 | `ReminderFlowUITests.testPopoverClearRemovesToken` 在 main 上存量失败 | 代码级问题，不是环境问题；恢复时必须先修，否则 UI 测试基线是红的 |
| #19 | Android FCM 兜底层研究 | 原本就延后到 v1 之后，随移动端一起归档 |

## 4. 目标功能（原本要做成什么样）

规格未删，完整定义看 [mobile-spec.md](mobile-spec.md)。这里只留索引：

| 想要的东西 | 规格位置 |
|---|---|
| v1 MVP 范围：浏览、重度编辑（实时/源码）、标签、设置与投递提醒、重复全量、可插拔双后端同步、今日到期小组件 | §5 |
| 五个里程碑的出口标准（M1 脚手架 → M5 Android 与收尾）| §7 |
| 双后端同步协议（GitHub A / 云盘 B）、冲突判定与留档 | §2.2、§10 D13 |
| 时间与提醒的共享契约：绑定 API、调度主循环、通知呈现 | §3 |
| 已定决策：D11 无 FCM、D13 冲突不 LWW、D17 组件交互、D19 免费签名边界、D21 里程碑编排 | §10 |
| 视觉基线 | [design-tokens-mobile-draft.md](design-tokens-mobile-draft.md) |

**明确从未纳入范围的**（不是欠账）：跨标签搜索、通知深链、应用内今日聚合页（三项 fast-follow）、
iPad 专属布局、手表、Siri/快捷指令、厂商推送通道、上架/TestFlight、坚果云 WebDAV、
iCloud for Windows、CloudKit、Syncthing、自建同步服务、Dropbox（§0、§2.2 末行）。

## 5. 恢复路径

**前置条件（二选一，没有第三条）**

- 开通付费 Apple Developer 账号：Time Sensitive entitlement 可用、签名一年有效；
- 或明确接受「7 天重签、只自用、不承诺长期可用」，那么 A2 与 A4 直接标为不适用，
  M3 出口降级为「重签当天可用」。

**顺序（不要跳步）**

1. `make mobile-framework` + `mobile.yml` 先跑绿，确认冻结期间 `core/` 的变动没有破坏绑定编译；
2. 真机验 **C1 + C4**。这两项决定 D21 的路线是否还成立；失败就关 #53、后端 B 回落 M5；
3. 真机验 A1、A3、A5–A10（A2 视账号而定），M3 出口收口；
4. 真机验 B1–B6（GitHub App 注册 + Device Flow + Keychain）；
5. 最后做 **E1–E4 双端联调**，这是 M4 出口的最后一环；
6. 然后才轮到 #26 人工手感与 M5。

**入口速查**

| 想找什么 | 去哪 |
|---|---|
| 目标与出口标准 | `docs/mobile-spec.md` §7、§8 |
| 各里程碑实现细节与证据 | `docs/reports/2026-10-2x-m1/m2…`、`2026-11-08-m3/m4-1/m4-2/m4-3…`、`2026-11-08-icloud-sync-probe.md` |
| 真机 runbook | `2026-11-08-icloud-sync-probe.md` 附录 A |
| 绑定构建 | `Makefile` 的 `mobile-framework`、`mobile/binding/go.mod` |
| iOS 工程 | `mobile/ios/project.yml`（XcodeGen 生成 `.xcodeproj`，生成物不入库）|

## 6. 冻结期间的约定

- 不修 iOS bug、不加 iOS 功能、不跑真机验收；`mobile/` 与 `mobile.yml` 原样保留。
- `core/` **不冻结**：桌面在消费它。改 `core/` 必须跑 `go test ./...`（含 core 契约测试），
  并注意 `mobile/binding` 依赖的 `zakkicore` 导出面。
- 桌面侧若改 `zakki.json` 格式或迁移语义，规格 §2.1 的共享契约仍受约束（ADR-0003 第 5 条）。
- issue [#26](https://github.com/gomeen/Zakki/issues/26)、[#50](https://github.com/gomeen/Zakki/issues/50)、
  [#57](https://github.com/gomeen/Zakki/issues/57)、[#19](https://github.com/gomeen/Zakki/issues/19)
  保持 open 作为恢复入口，不进入 triage 队列。

## 7. 不涉及手机端的剩余工作

移动端冻结后仓库里剩下的事，按「值不值得做」粗排：

| 待办 | 现状 | 涉及面 |
|---|---|---|
| [#22](https://github.com/gomeen/Zakki/issues/22) Windows 通知实机 + macOS 通知中心视觉确认 | 清单已写好（`docs/verification/notifications-manual-checklist.md`），只有参数/脚本单测与交叉编译证据 | 需一台真机 Windows + macOS |
| 桌面同步真实账号验证 | 桌面 GitHub 后端 A 已发布，但 Device Flow、token 过期/撤销、限流、>1MB raw 回退仍只有 mock 证据 | 纯桌面，1 个 GitHub App 即可开始 |
| Windows DPAPI 安全存储 | 交叉编译 + 接口测试过，**从未在真实 Windows 上运行** | 纯桌面，需一台 Windows |
| Copilot 式输入补全 | 调研报告已完成（`docs/reports/2026-11-11-copilot-style-input-research.md`，未提交），MVP 建议在报告 §6.4；硬缺口是 MyGo 没有 ghost text 装饰层 | 纯桌面，需先决策 |
| [#49](https://github.com/gomeen/Zakki/issues/49) Listed on Awesome MyGo | 对外列表登记，非代码 | 元数据 |
| 性能与启动的后续建议 | 见 `docs/reports/2026-10-18-performance-review.md`、`2026-10-18-startup-shutdown-review.md` 的「后续建议」：大表格块内虚拟化、镜像扫描与 Git runner 的超时、备份同步入口后台化、迁移改元数据预检 | 纯桌面，均非阻塞 |
