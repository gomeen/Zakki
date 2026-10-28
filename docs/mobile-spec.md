# Zakki 移动客户端规格（iOS 先行 + Android 对齐）

> 状态：**规格已确认；移动端尚未实现**。本文是
> [#1「手机提醒与 iOS 客户端路线」](https://github.com/gomeen/Zakki/issues/1) 的目的地产物，
> 汇总 #3–#12 研究与定案，并按 [2026-10-28 review](reports/2026-10-28-reminders-mobile-review.md)
> 与 [#16](https://github.com/gomeen/Zakki/issues/16) 校正；D11、D13、D17 已定案，见 §10。
> 术语见 [GLOSSARY.md](../GLOSSARY.md)，时间与重复语义以
> [ADR-0001](adr/0001-inline-time-tokens.md) 和 [ADR-0002 修订节](adr/0002-recurring-time-tokens.md#2026-10-28-修订统一实例语法与日历边界)
> 为准；视觉基线见 [design-tokens-mobile-draft.md](design-tokens-mobile-draft.md)。

## 0. 目标与范围

在 iPhone 先提供浏览、重度编辑、标签、提醒（含重复）和同步，再让 Android 效果对齐。
双端与桌面共享 `zakki.json` 和同一套时间语义。本文中的「必须」是实施验收要求，不是完成声明。

- 桌面重复的最小配套属于本分支实施范围，见 §6；桌面 OS 级离线调度仍不在 v1。
- v1 不含跨标签搜索、通知深链、应用内今日聚合页（这三项 fast-follow）、iPad 专属布局、
  手表、Siri／快捷指令、厂商推送通道、上架／TestFlight，也不含 FCM 兜底
  （延后为独立票 [#19](https://github.com/gomeen/Zakki/issues/19)，见 §10 D11）；
  v1 只用本地闹钟，加上打开应用时对账。

## 1. 技术栈与工程结构

- iOS：SwiftUI 壳 + UIKit 文本栈（UITextView、NSTextAttachment 行内组件）；已有 spike
  原型验收只证明交互方向，不代表正式客户端完成。
- Android：Kotlin + Jetpack Compose；设计令牌和验收保证与 iOS 一致，M5 才进入完整客户端实现。
- 共享内核 `core/` 是桌面仓库内的**嵌套 Go module `zakki/core`**，零第三方依赖，禁止依赖
  MyGo 和 `zakki/internal/*`。目标绑定目录／包名为 **`core/zakkicore` / `zakkicore`**，
  gomobile 输出 XCFramework／AAR。已有绑定构建实验不等于移动产品落地。
- 绑定构建模块与桌面**同仓**：`mobile/binding` 是独立 Go module（模块路径
  `zakki/mobile/binding`），其 go.mod 声明 `require zakki/core` 并
  `replace zakki/core => ../../core`（replace 相对包含它的 go.mod 所在目录解析）；
  `golang.org/x/mobile` 只出现在该模块，经 tool directive 固定版本。
  不通过 `go get zakki/core` 获取，也不把本地 replace 当作版本锁。升级以 mobile/binding
  内固定的 x/mobile 版本与重建产物一起评审（R10）。
- iOS 工程在 `mobile/ios`，用 XcodeGen 维护（`project.yml` 生成 .xcodeproj，
  生成物不提交）；绑定产物（XCFramework／AAR）不入库，CI 重建验证。
- 移动 CI 检查绑定产物、Go／gomobile 版本与客户端编译；桌面 `make check` 继续测试 core。
  gomobile 仍属 experimental，工具链升级必须重建双平台产物。
- 分发：iOS 免费个人签名，按 7 天重签演练；Android APK 侧载。v1 不上架。

## 2. 数据模型与同步

### 2.1 数据与兼容（D14、D18）

唯一笔记文件 `zakki.json` 当前 version 3，形状为
`{version, active, tabs:[{id,name,text,color}], settings}`；正文 Markdown 是时间语义唯一存储。
`preferences.json` 为设备偏好，不直接作为共享笔记同步。

M1 拟从 `internal/notes/file.go` 提取 core 的 model 能力，统一 File／Tab／Settings 与迁移语义；
这是未来结构迁移，实施提交需同步工程与架构文档，本次只修规格。

- 文件版本高于客户端支持版本时进入写保护：保留原始字节，禁止编辑、迁移、同步上传和自动覆盖，
  显示升级／重读入口。损坏文件、标签超限也不得截断后写回。
- 支持版本内的未知字段必须在所有层级保留并往返（顶层、settings、tab 等）；采用 RawMessage
  或等价未知字段容器，禁止强类型解码后静默丢键。修改已知字段不能重置未知字段。
- 数据升版本须协调桌面与移动端的读取能力、迁移测试与回退保护，不能靠规则语法版本替代。
- settings 中 `alwaysOnTop`、`hideOnEscape` 等桌面专属项在移动端不提供修改入口、不赋移动默认值，
  原值和未知项原样往返；移动 UI 的有效偏好单独留在设备，不能覆盖桌面配置。
- 后续事项：同一数据版本内各端写的已知键相同。M1 起移动端经 `core/model` 的
  Decode/Encode 保留未知字段读写；桌面后续也切换到这套读写（当前 save/load 仍是
  `json.MarshalIndent`／强类型解码，会把移动端保留的未知字段在下次保存时丢弃），
  已单独开票 [#24](https://github.com/gomeen/Zakki/issues/24)。

### 2.2 同步协议（双后端，任一时刻只激活一个）

| 后端 | 覆盖与并发边界 | 认证 |
|---|---|---|
| A. GitHub Contents API | 全平台；sha CAS，冲突拒绝后重新拉取并进入冲突处理 | GitHub App user token，Device Flow |
| B. 云盘文件夹 | 以各平台实际可访问的文件提供方为准；打开对账与冲突副本 | 系统文件授权 |

后端 B 不是「macOS 只能 iCloud、Windows 只能 OneDrive」；OneDrive 可跨平台，iCloud Drive
需核对个人团队 capability 与文件选择器授权，第三方 provider 不保证 NSFileVersion。

- 同步在前台／聚焦时拉推；成功应用远端内容后立即全量对账提醒，不承诺持续后台同步。
- **开启同步即暂停桌面 git.autoCommit 和 git.autoPush 两个旧调度器**，UI 明确说明暂停原因、
  当前后端和同步状态；不悄悄改写用户原开关。关闭同步后按保留配置恢复备份功能。
- 后端 A 每轮**先 pull（读取最新远端及 sha）、处理本地待保存与冲突，再 commit（Contents API
  带最新 sha 上传）**；CAS 失败重新 pull，不盲重试旧 sha。此处 pull 指读取远端文件，不是让
  现有 Git 自动备份继续只 push。同步前后的本地落盘与上传必须串行，不混跑两条提交链（D12）。
- 桌面未来新增 sync 偏好节，按当前偏好版本递增（原计划 v14，实施时核对）；凭据不进入该节。
  云盘更新仅在无未保存本地修改且校验通过时安全跟随；其余进入冲突／写保护处理。
- 凭据：iOS Keychain，Android Keystore 保护的加密存储；GitHub App 安装仅授权所选仓库，
  用可过期、可刷新／撤销的 user token，不用拥有全部仓库权限的 OAuth repo token。
  不将 token、刷新令牌写入 zakki.json、prefs、日志或 Git（R7）。
- 冲突处理定案（D13，2026-10-28）：**同步基线 + 冲突由用户选择**，不依赖任何设备时钟。
  基线是每台设备本地记录的上次同步成功内容 sha256 与后端版本号（后端 A 为 blob sha，
  后端 B 为文件身份与修改时间，修改时间只作变化提示）；基线不写进 zakki.json、偏好文件或 Git。
  判定（「已变」指与本地基线比较；冲突按整个文件处理）：

  | 本地 | 远端 | 处理 |
  |---|---|---|
  | 等于基线 | 已变 | 采用远端，前提是没有未保存的本地修改 |
  | 已变 | 等于基线 | 上传 |
  | 已变 | 已变，与本地内容相同 | 只更新基线 |
  | 已变 | 已变，与本地内容不同 | 弹冲突对话框：保留本机，或保留远端 |

  云盘服务自己生成的冲突副本（如 `zakki (1).json`）当作远端候选，走同一个对话框。
  落选的版本完整写入与 zakki.json 同目录的 `conflicts/zakki-YYYYMMDD-HHmmss-<设备名>.json`，
  随同步一起保存；留档不进标签、不参与调度、不计入 9 个标签上限，因此没有标签 ID 冲突，
  也不会重复提醒；设置里可查看和删除，不自动清理。
  `active` 字段继续共享：移动端原样往返、不修改；移动端当前打开哪个标签记在设备本地。
- 不做坚果云 WebDAV／ETag 实测、iCloud for Windows 实测、CloudKit、Syncthing、自建同步服务、Dropbox。

## 3. 时间与提醒共享契约

### 3.1 职责边界（D3）

core 唯一拥有时间令牌簇的识别／规范写回、提醒档位白名单、严格规则校验、日历实例展开、
当前实例及推进、规则语法版本和所选时区。禁止 Swift／Kotlin 复制 parseRecur 或另写时间正则。

客户端负责 Markdown 块分类、围栏／缩进代码和表格排除、行内代码／链接保护区、任务勾选状态、
通知可读正文投影、组件显示、文档事务／撤销、同步与 OS 调度。通知正文取同一条内容中组件
前后的全部可读正文，去掉令牌及 Markdown 标记，链接取显示文字；不拼入其他条目。

### 3.2 gomobile 绑定 API（目标契约，D4–D7）

下列函数位于 `core/zakkicore` 包，参数均为可绑定的 string／int／int64。
RuleSyntaxVersion 返回 `(int, error)`，其余返回 `(string, error)`；JSON 只跨边界，不导出 Go 切片／结构体。
所有函数均在入口设 recover 防线，panic 转 `internal: 内部处理异常`，包括版本读取函数；
客户端按错误码处理，不匹配中文文案。

| 函数 | 参数与结果 |
|---|---|
| `FindTimeToken(line, protectedJSON)` | 返回 `{found,start,end,suffix}`；未找到为 `{found:false,start:0,end:0,suffix:null}`，不报错 |
| `ReplaceTimeToken(line, suffixJSON, protectedJSON)` | 返回修改后的文本；suffix 为 `null` 或空串时清除首个合法令牌；无令牌且非空则按规范追加，组件后正文保持原位 |
| `SplitTimeSuffix(line)` | 返回 `{text,suffix}`；只在首个合法令牌位于行尾时剥离，否则原文与 null，不改选最后一个 |
| `JoinTimeSuffix(text, suffixJSON)` | 传入已剥离／不含令牌的正文，按 `@ ~ ^` 写回；null／空串只保留正文，不作为任意行内替换 API |
| `ParseRule(rule)` | 返回 `{every,unit,weekdays?,end?}` JSON；整段严格校验；非法规则报错 |
| `Expand(ruleJSON, anchorMs, nowMs, horizonDays, maxCount)` | 返回升序 Unix 毫秒数组，范围 `(now, now+horizonDays 个日历日]`；空结果、非正地平线／数量为 `[]`；上限 3660 天／1000 条，超限报错 |
| `Occurrence(ruleJSON, anchorMs, nowMs)` | 返回 `{dueMs,ended}`；不晚于 now 最近实例，无则首个；超过 end 仍返回最后实例；不存在合法实例才 ended |
| `Advance(ruleJSON, anchorMs, nowMs)` | 返回 `{dueMs,ended}`；当前实例之后首个合法实例；越 end／无后继则 ended，不受 Expand 地平线限制 |
| `AlertTiers()` | 返回 `[{suffix,ms}]`（例 `[{"suffix":"0m","ms":0},…]`），ms 为提前量毫秒 |
| `SetLocation(tzName)` | 设置 IANA 时区，成功返回空串；非法名称报错且保留旧时区；空串恢复 Go time.Local |
| `RuleSyntaxVersion()` | 返回整数 `1`，以函数读取，不以 gomobile 常量导出替代 |

suffix 只有一套形状，所有字段均输出，不使用 omitempty：

```json
{"due":"2026-10-29 10:00","dueMs":1793268000000,"alertMs":600000,"hasAlert":true,"recur":"^1d"}
```

示例 dueMs 对应 UTC；实际以 SetLocation 选定时区计算。`due` 是分钟精度墙上时刻；`dueMs`
**仅输出、忽略输入**，写入由 due 和所选时区重算。`recur` 为空串表示不重复。
`hasAlert=false` 要求 alertMs=0；true 时 alertMs 必须是 AlertTiers 的 ms 白名单，0 表示准点。
非法 due、白名单外提前量、矛盾开关或非法 recur 均报错，不静默删除字段。
无令牌结果和未知 `^` 整簇普通文本属于正常解析结果；直接传非法 suffix／rule JSON 是调用错误。
ended=true 时 dueMs=0，客户端不可将其排为 epoch 通知。

`start/end` 和 `protectedJSON` 均为 **UTF-16 code unit 半开区间 `[start,end)`**，与 Swift
NSRange／Kotlin String 索引对齐。protectedJSON 为 `[[start,end],…]`，空串／null 等价空数组；
客户端传入当前行的 code／link 区间，不能省略这些保护区后声称具备 Markdown 排除能力。
越界、倒序、割裂 surrogate pair 返回错误；core 内部字节／rune 下标不能泄露为绑定偏移。
Split／Join 用于客户端已确认可处理的正文行；Find／Replace 在同一保护区输入下判定一致。

| 错误码 | 中文说明 |
|---|---|
| `invalid_json` | JSON 无法解码或形状不符 |
| `invalid_due` | due 缺失、格式或时刻非法 |
| `invalid_alert` | 提前量不在白名单或与开关冲突 |
| `invalid_recur` | suffix 中的重复规则非法 |
| `invalid_rule` | 规则文本或 ruleJSON 语义非法 |
| `invalid_offset` | UTF-16 区间非法 |
| `invalid_args` | 参数超限或时区非法 |
| `internal` | 内核执行异常（recover 兜底） |

错误格式固定 `code: 中文说明`，具体上下文可附在中文说明之后；内部 Go 错误也需转为该格式。
每函数独立 recover，不能只靠应用最外层捕获。空结果与错误必须分开处理。

Android 必须在**启动及 TIMEZONE_CHANGED** 时先取得系统 IANA 时区并调用 SetLocation，再做任何
解析／实例查询／重排，不能用 Android 上可能固定 UTC 的 Go time.Local 当默认本地时区。
iOS 也在启动和系统时区变化时注入。一次批次使用一致时区快照，切换成功后作废旧批次并重排。

本分支已对齐绑定目录 `core/zakkicore`、AlertTiers 的 `ms` 键、RuleSyntaxVersion 函数及上述
DST 行为，并通过 Go 契约测试；这不表示移动工程、双平台绑定产物或客户端已经实现。

### 3.3 调度主循环

打开／回前台 → 拉取并应用同步 → 采集不可变条目快照 → core 展开／查询 → 对账重排通知。
没有 `~`、已完成任务、未知规则整簇均不调度；fireAt 与首次启动／会话内立即投递边界严格沿用
ADR-0001。当前实例／推进沿用 ADR-0002 修订节；重复任务的当前实例在今天时显示今天，
更早或结束的实例保持中性，不用历史锚点染过期色。

- 双端统一**展开单次实例，不用原生 repeating trigger**（D16）。地平线按 due，不按 fireAt；
  窗口外实例不能因提前量跨日自动挤入。桌面默认 60 天，客户端刷新时补排窗口。
- iOS 64 个 pending 槽：跨全部条目合并后按 fireAt 从早到晚分配，平局以稳定条目身份排序，
  不让先扫描的任务独占。需要后续续排时，最后一个槽保留为「打开 App 续排」通知，其余至多
  63 个给最早触发实例；全部实例可容纳则无需占续排槽。展示已排定数量、覆盖到的时刻与待续排
  状态；打开后清理续排通知并补足。续排通知不能自动扩大系统后台预算（R5）。
- iOS 拟使用 Time Sensitive，须先证明免费 Personal Team 可用；不可用则普通通知并明确展示
  权限限制，不承诺其绕过所有免打扰策略（R2）。
- Android 注册下一个精确闹钟，触发后 receiver 投递并注册下一实例；重排依赖本地持久快照。
  侧载 v1 在 API 33+ 以 **USE_EXACT_ALARM 为主**，API 31–32 或不适用环境用
  SCHEDULE_EXACT_ALARM 特殊访问兜底，调用前核对能力并处理 SecurityException；拒绝时展示
  无法精确排定／降级状态，不能悄悄显示成功。未来进入 Play 必须重新评估政策资格与权限方案（D15）。
- Android 使用 setAlarmClock() 的 Doze 通道，并说明系统闹钟图标；13+ 单独请求
  **POST_NOTIFICATIONS**。监听 BOOT_COMPLETED（声明 RECEIVE_BOOT_COMPLETED）、
  MY_PACKAGE_REPLACED、TIME_SET、TIMEZONE_CHANGED 重建；时区广播先 SetLocation。
  force-stop 会断开触发后续排链，必须再次打开恢复；WorkManager 只做同步辅助，不替代精确提醒（R6）。
- 完成重复任务通过 Advance 改写锚点并保持未完成；ended 才标记完成。非任务不能推进，
  普通任务完成后不再排定；移动端按 §4 的 D17 定案交互：任务只能通过勾选框完成，
  点时间组件打开时间弹层。
- 每次对账撤销失效实例、应用 ADR-0001 的本地去重；多设备各响一次是已接受风险（R9）。

### 3.4 通知呈现

纯系统通知；点通知打开 app，深链 fast-follow。通知使用客户端投影的可读正文。
UI 区分「已排定」「待同步／待续排」「权限受限」，不将写盘成功等同于系统已接受通知。

## 4. UI 与视觉规格

- 一级标签列表：彩色胶囊，颜色对齐桌面；二级为单面实时编辑器，支持横屏与源码切换。
- 令牌簇在缓冲中为单个 U+FFFC，磁盘保留原始 Markdown；选区、剪贴板、删除和撤销必须往返。
  键入完整合法簇可收为组件；未知簇按普通文本保留。
- 底部 Markdown 快捷栏保留标题、列表、任务、引用、粗体、代码、插入时间入口。
- 组件交互定案（D17，2026-10-28）：点时间组件（任务行与非任务行同样）打开时间弹层，
  可编辑时刻、提醒与重复；任务只能通过勾选框完成，带重复的任务勾选即推进（Advance）；
  不存在「点组件即完成」的操作。
- 验收按桌面风格与排版主题，原型占位值不作为最终标准。M2 从桌面提取最小主题集
  （明暗、正文／标题／任务／引用／代码／组件、标签色板）；完整经典／手账、代码配色、
  手账装饰和字体分发另行细化，不把现有主题数据说成已在 core 中共享。

## 5. MVP 清单

浏览、重度编辑（实时／源码）、标签、设置与投递提醒、重复规则全量、可插拔双后端同步；
小组件第一批显示今日到期提醒（iOS WidgetKit／Android AppWidget，M5 细化）。
不做清单见 §0；FCM 兜底已定延后为独立票 [#19](https://github.com/gomeen/Zakki/issues/19)（§10 D11），不在 v1 交付范围。

## 6. 桌面端配套与落地边界

本节区分本分支桌面实施和未来移动实施，不以规格更新充当代码完成证明。

1. **本分支桌面重复最小配套已实现**：消费 Occurrence 展示当前实例且不标过期；超过 end
   仍取末次实例并以中性 chip 呈现；完成走 Advance，无后继则结束；按实例应用提前量调度；
   弹层提供预设和自定义重复规则，未改规则保留 `^` 原文。主窗口销毁后仍从不可变快照续排，
   勾选及弹层写回均可撤销。自动化与原生验收记录见
   [本次验证记录](reports/2026-10-28-review-remediation-verification.md)；此配套不等待 M4，
   桌面 OS 级离线调度仍不在范围内（D1）。
2. **共享契约对齐**：§3.2 的绑定目录、字段、版本函数与 DST 行为已随本分支完成并通过
   Go 契约测试；未知簇、UTF-16、秒清零与日期边界按 ADR 执行，移动产物验收另在 M1。
3. **未来 M4 同步配套**：新增 sync 偏好；暂停旧 autoCommit／autoPush 并提供 UI 说明；
   后端 A 先 pull 再 commit；云盘安全重读与冲突呈现。当前桌面仍是 Git 备份，未实现该同步循环。
4. **未来 M1 模型迁移**：已知／未知字段往返与高版本写保护；结构迁移时更新 architecture 和工程规则。

## 7. 里程碑

| 里程碑 | 内容 | 出口标准 |
|---|---|---|
| M1 脚手架 | iOS 工程、同仓 mobile/binding + replace、目标绑定／XCFramework、model 提取、CI；系统文件选择器导入 zakki.json | 模拟器显示真实导入文件的标签；高版本写保护、未知字段／桌面设置往返测试通过 |
| M2 编辑器 | iOS 双模式、标签、最小主题集、导出本地文件 | 长中文重度编辑由用户验收；M4 前经系统文件导入／导出手动交换，不承诺自动回传桌面 |
| M3 iOS 提醒 | iOS 令牌弹层、单次通知重排、完成推进、重复、Time Sensitive 能力验证 | **仅 iOS** 到点响、槽位策略、重签／重启分场景恢复、状态展示通过；不要求 Android 出口 |
| M4 同步 | GitHub 后端、§2.2 的冲突处理与留档、桌面同步配套 | 桌面 ↔ iPhone 双向同步、CAS 冲突与旧 Git 暂停可复现；§2.2 判定表四种情况与 conflicts/ 留档生成可复现 |
| M5 Android 与收尾 | Android 工程／编辑器／AAR／闹钟／同步对齐，云盘后端、双端小组件、分发流程 | Android 全部权限／生命周期检查及云盘、签名、小组件清单通过 |

每个里程碑开实施票；未决决策阻塞相应出口，不能由实现者擅自选定。共享语义缺口回 core 修。

## 8. 实施验证清单

- [ ] M1：同仓 mobile/binding + replace 可复现，x/mobile 版本在其 go.mod 固定，replace 相对路径正确；双平台绑定构建与 Go／gomobile 升级回归。
- [ ] M1：高版本写保护；未知字段嵌套往返；桌面专属 settings 原值保留；超过 9 标签不丢数据。
- [x] 工程守护 #17：core 与 vendor/zakki/core 的共享非测试源码一致性检查进入 make check；
  排除绑定目录及模块元数据，故意改坏副本或添加额外源码会使检查失败；core 另在纽约时区回归（R3）。
- [ ] M3／M5：Find／Split／Replace 一致性，UTF-16 中文／emoji 保护区，未知规则整簇普通文本，
  AlertTiers 形状、每函数错误码与 recover；DST gap 归一化后恢复钟点、每月／闰日跳过、Every 上限。
- [ ] M3：Personal Team 下 Time Sensitive entitlement 真机／Xcode 实测；不支持时普通通知降级和状态文案（R2）。
- [ ] M3：profile 过期检测与提示；同 bundle 覆盖重签、覆盖安装、卸载重装分别测数据／授权／pending
  行为，不预设「重签一定清零」；演练免费账号 7 天流程及 App ID／每设备 App 配额（D19、R5）。
- [ ] M3：64 槽跨任务按最早 fireAt 分配、最后槽续排、窗口边界；已排定／待同步／待续排状态展示。
- [ ] M3／桌面：睡眠、唤醒、手动改时、时区改变、连续编辑跨触发点；首次启动不补发与会话宽限（R1）。
- [ ] M4：GitHub 蜂窝真机可达性、CAS 冲突、token 过期／刷新／撤销、Keychain／Keystore 安全保存；
  同步开启暂停两个旧 Git 调度器，关闭恢复，远端更新后能先拉取再提交；§2.2 判定表的四种情况
  与 conflicts/ 留档文件生成可复现。
- [ ] M5：Android 12+ 精确权限拒绝／撤销与降级；API 33+ USE_EXACT_ALARM 主路径、兜底特殊访问；
  POST_NOTIFICATIONS 拒绝、通知通道关闭、Doze、setAlarmClock 图标、force-stop 后再次打开恢复。
- [ ] M5：BOOT_COMPLETED／MY_PACKAGE_REPLACED／TIME_SET／TIMEZONE_CHANGED 冷启动重建；
  Android 时区 SetLocation 与 receiver 冷启 Go 耗时，16 KB 页大小设备加载 AAR（D19、R6）。
- [ ] M5：OneDrive 国行网络可达性、冲突副本命名／触发；iCloud 和第三方 provider 的差异、
  NSFileVersion 恢复入口、仅云端占位下载、第二台桌面加入已有文件、写保护与迁址失败（R8）。
- [ ] M5：个人团队 App Group／WidgetKit capability、共享文件授权、小组件加载 Go 运行时与内存预算；
  不可用时呈现具体限制再决定替代路径，不默认可用（D19、R5）。
- [x] 桌面：行尾输入 `^w@` 不误开时间弹层（实时预览的行尾 `@` 触发会跳过 `^` 开头的片段；源码模式无自动弹层）；
  继续输入周几集、普通行尾 `@`、行内代码内 `@` 均有回归（R13）。

## 9. 风险与已接受取舍

- R1：桌面依赖应用存活，睡眠／改时／时区可能造成迟到；30 秒巡检和唤醒对账缓解，不补偿无限历史。
  Android 的系统时区必须显式注入，不能依赖 Go 启动时的本地时区。
- R4：地平线按实例时刻，提前量跨窗的通知可能尚未注册；定期补排并展示覆盖范围。古老锚点不可
  线性逐期追赶，实施时测长序列开销与查询延迟。
- R5：iOS 64 槽与免费签名限制仍存在；续排槽需要用户打开，不能保证长期不开 app 仍完整续排。
  profile 失效、重签和卸载行为以实测分开描述；Time Sensitive 和 App Group 不预先承诺可用。
- R6：Android force-stop／厂商自启动限制会断链；v1 不做 FCM 兜底（D11），桌面的改动要等下次
  打开手机同步对账后才生效，这一延迟已接受。普通清理与 force-stop 分开测试，
  不把 setAlarmClock 当作所有 ROM 上的必达保证；FCM 在无 GMS 设备不可用的取舍随独立票（#19）跟进。
- R8：云盘替换文件可能触发桌面 verifyDisk 写保护，云端占位不是可读文件；迁址拒绝覆盖已有目标，
  第二台设备加入不能直接假设迁址可用。文件提供方能力、原子发布和 capability 须验证后设计入口。
- R9：**接受多设备同一实例各响一次**，本地去重不跨设备协调；冲突留档文件（D13）不参与调度，
  不会因此重复提醒。不能把正常多设备重复的接受扩大为默认接受冲突副本重复。
- R10：gomobile 工具链及运行时体积随内核增长；绑定构建模块与桌面同仓，x/mobile 版本在
  mobile/binding/go.mod 固定，绑定产物不入库、CI 重建验证。
- R13：`^w@` 与行尾 `@` 自动弹层的歧义已在桌面修复：光标前的非空白片段以 `^` 开头时不触发。
  移动端若实现同类行尾触发，须沿用同一规则。
- 双原生有两套 UI 成本，已接受；桌面改动到手机仍受同步时机限制，属于尽力而为。
  R11／R12 已随评审整改落地（快照与后台解析 `2a84322`；停止后保存、推送拒绝、硬链接回退
  `d519362`、`7d0415b`、`45f80e2`）；R14 提交分类教训由工程流程跟进。

## 10. 已定决策记录

| 项 | 结论（2026-10-28 定案） |
|---|---|
| D11 FCM 兜底 | 延后为独立票 [#19](https://github.com/gomeen/Zakki/issues/19)；v1 只用本地闹钟，加上打开应用时对账；桌面的改动等下次打开手机才生效，已接受（R6） |
| D13 同步冲突 | 同步基线 + 冲突由用户选择，不采用自动 LWW；基线是设备本地记录的上次同步成功内容 sha256 与后端版本号；冲突按整个文件处理并弹对话框二选一；落选版本完整留档到 conflicts/，不进标签、不参与调度、不占标签上限；active 字段继续共享、移动端原样往返，当前标签记在设备本地（§2.2） |
| D17 组件交互 | 点时间组件（任务行与非任务行同样）打开时间弹层，可编辑时刻、提醒与重复；任务只能通过勾选框完成，带重复的任务勾选即推进（Advance）；不存在「点组件即完成」的操作（§4） |

规格已确认；#16 与 #8 随 D11／D13／D17 定案关闭，移动端实现按 §7 里程碑另开实施票。
