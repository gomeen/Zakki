# 提醒实现与移动端规划全面审查（2026-10-28）

审查对象：提交 `8aad0ea` 时的桌面提醒实现、共享内核 `core/`、移动端规划（[#1](https://github.com/gomeen/Zakki/issues/1)–[#12](https://github.com/gomeen/Zakki/issues/12)、[mobile-spec.md](https://github.com/gomeen/Zakki/blob/afe363348864e4901e937690cf0c802067a5d27a/docs/mobile-spec.md)、[ADR-0001](../adr/0001-inline-time-tokens.md)、[ADR-0002](../adr/0002-recurring-time-tokens.md)、2026-10-27 各报告、[design-tokens-mobile-draft.md](https://github.com/gomeen/Zakki/blob/afe363348864e4901e937690cf0c802067a5d27a/docs/design-tokens-mobile-draft.md)）与上述代码的测试覆盖。

方法：只读审查，可疑边界写临时探针程序（/tmp 独立模块，`replace` 指向 core 或仓库副本）实测，跑完删除；仓库未改动。范围内 `go test`、`-race`、`-count=3`/`-count=15` 均通过，无不稳定用例，下列问题都是现有测试没走到的路径。

证据标注：**〔复现〕** 写程序实际运行确认；**〔静态〕** 读代码推断；**〔源码〕** 核对 Go 工具链源码；**〔网络〕** 第三方资料，需真机或 Xcode 确认。行号以 `8aad0ea` 为准。

## 1. Bug

### 1.1 重复规则与 core / bind

| # | 问题 | 出处 | 证据 |
|---|---|---|---|
| B1 | 时间弹层点「完成」删掉 `^` 重复段：构造 `TimeSuffix` 未带 `Recur`，`liveBlock` 也无该字段；移动端写入的 `^w@1-5` 被桌面一次点击抹掉 | `internal/app/live_timepopover.go:249`、`internal/app/live_blocks.go:29-32` | 〔复现〕 |
| B2 | `SplitTimeSuffixJSON` 的 `alertMs` 输出纳秒（`int64(lt.Alert)`，`~10m` → `600000000000`），读入侧按毫秒；Split→Join 往返后提醒被静默删除 | `core/bind/bind.go:56` 对 `:47` | 〔复现〕 |
| B3 | `Expand` 不校验传入 JSON：`every` 为 0/负数时 `d`/`w` 死循环，`mo`/`y` 在 `k%Every` 除零 panic（gomobile 不转 error，即闪退）；`weekdays:[9,0]` 输出乱序；`every>1` 配周几集时间隔被静默忽略 | `core/recur.go:111`、`:142`、`:167`、`:175`、`:148-165` | 〔复现〕 |
| B4 | 超大间隔卡死：`^9999999999y`、`^100000000mo` 能过 `ParseRule`（次数无上限），循环逐月/逐年走，>5 s 不返回；移动端「打开即全量重排」会卡死主线程 | `core/recur.go:56`、`:173-204` | 〔复现〕 |
| B5 | Android 上 Go 的 `time.Local` 固定为 UTC（`initLocal` 写死 `localLoc = *UTC`，golang/go#20455）；core 全部用 `time.Local`，中国用户时刻偏 8 小时 | `$GOROOT/src/time/zoneinfo_android.go:21-24`；`core/timetoken.go:104/172`、`core/recur.go:119/137/215`、`core/parsejson.go:36`、`core/bind/bind.go:43` | 〔源码〕 |
| B6 | 夏令时春跳后 `^1d`/`^Nw` 永久漂移 1 小时：在上一实例上累加 `AddDate`，不存在的 02:30 被归一化后成为新基准（纽约时区 03-08 起恒为 01:30）；周几集/`mo`/`y` 用 `clock()` 从锚点重建，不受影响。另 `d`/`w` 保留锚点秒数，周几集清零 | `core/recur.go:142`、`:167` | 〔复现〕 |
| B7 | `maxCount<=0` 仍返回 1 个实例（先追加后判断），iOS 槽位用尽时多排一条 | `core/recur.go:127-135` | 〔复现〕 |
| B8 | 无实例时返回 `null` 而非 `[]`（`var out []int64` 为 nil），Swift/Kotlin 按数组解码失败 | `core/recur.go:125`、`:211` | 〔复现〕 |
| B9 | `ParseRule` 过松：结束日期只查长度 10；`Sscanf` 不要求读尽。`^w@1-5x`、`^w@1-5#2`、`^1d<abcdefghij`、`^1d<2025-13-45`、`^1d< 2025-12-31` 均被接受并被 `FindTimeToken` 纳入令牌，展开时才报错；`#2` 这类未来扩展语法被误解析成旧规则，破坏 ADR-0002 的前向兼容 | `core/recur.go:34-39`、`:80`、`:92` | 〔复现〕 |
| B10 | 提醒档位按前缀匹配、无边界检查：`~10min` 认成 `~10m`，`Replace(nil)` 后正文剩 `a in`；`~1hour`、`~1h30m`、`~0m5` 同理。违背「不做部分识别」，且与 `SplitTimeSuffix` 结论相反 | `core/timetoken.go:189-196` | 〔复现〕 |
| B11 | 移动端入口 `core.ParseTimeToken` 是独立正则实现，与桌面 `FindTimeTokenEx` 至少 8 类差异：接受白名单外 `~7m`、`~90m`；`~999999999999999d` 溢出为负；`\@` 转义无效；行内代码内令牌被识别；`10:001` 被接受；首个日期非法即报错不继续找；`… 正文 ^1d` 隔正文吸收；`^hello` 让整行报错（桌面忽略该段）。违背 spec「零语义漂移」；`:43` 每次调用重编译正则 | `core/parsejson.go:23-66`；`docs/mobile-spec.md:66` | 〔复现〕 |
| B12 | `ReplaceTimeTokenJSON` 传 `protected=nil`，会改写反引号里的示例而非真正的令牌 | `core/bind/bind.go:69` | 〔复现〕 |
| B13 | 夏令时空档时刻三入口三结论（纽约 `@2025-03-09 02:30`）：Split 接受并规范成 01:30，往返改写源码；Find 因往返校验拒绝，`Replace` 于是行尾追加第二个令牌；`ParseTimeToken` 的 `due` 与 `dueMs` 互相矛盾 | `core/timetoken.go:104`（缺 `:172` 的往返校验） | 〔复现〕 |
| B14 | 非规范顺序 `@… ^1d ~10m`：Find 只认到 `^1d`，`Replace` 写成 `~10m ~10m`，`Replace(nil)` 剩 `a ~10m`；Split 对同一行返回 nil | `core/timetoken.go:184-207` | 〔复现〕 |
| B15 | 无法识别的 `^` 段被忽略但令牌仍成立，会在锚点响一次；ADR-0002 要求「不触发」，未来新规则在旧桌面退化为一次性提醒 | `core/timetoken.go:202-207`；ADR-0002:24-25 | 〔复现〕 |

### 1.2 桌面调度与通知

| # | 问题 | 出处 | 证据 |
|---|---|---|---|
| B16 | 退出被取消后提醒永久停用：先 `stopReminders()`（`connected=false`），随后迁址中/保存失败/偏好保存失败返回错误取消退出，恢复路径不重连，之后 `rescheduleReminders` 直接 return | `internal/app/state.go:264-265`；`internal/app/updates.go:38-44` | 〔复现〕 |
| B17 | 删标签、重命名、载入、重读都不重排：只有 `onEdited` 调 `rescheduleReminders`。已删除标签的提醒照弹；通知用旧标题；重读进来的提醒在下次编辑前不排，被外部删掉的旧定时器仍触发 | `internal/app/tab_model.go:201`、`:294`、`:482`；`internal/app/state.go:92`、`:116`；`internal/app/view.go:589` | 〔复现〕 |
| B18 | 连续输入跨过触发时刻提醒丢失：编辑时立即作废旧定时器，250 ms 防抖后新快照又把 `fireAt<=now` 当错过丢弃（实测触发前 300 ms 起每 100 ms 编辑一次，0 条通知） | `internal/app/reminders.go:82`、`:113` | 〔复现〕 |
| B19 | 提前量大于剩余时间时完全不提醒（5 分钟后的事设 `~10m`），无降级、无提示 | `internal/app/reminders.go:112-114` | 〔复现〕 |
| B20 | 跨软换行的令牌：连接后的正文能找到令牌并调度，弹层在含 `\n` 的原文里找不到，写回后重复追加令牌（`…10:00 ~10m @2024-06-02 10:00 ~1h`） | `internal/app/live_blocks.go:161`；`internal/app/live_timepopover.go:436` | 〔复现〕 |
| B21 | 引用内已勾选任务 `> - [x] …` 按引用块处理，完成状态不生效，仍触发提醒；缩进代码块不被识别为代码 | `internal/app/live_blocks.go:127` | 〔复现〕（引用部分） |
| B22 | macOS 通知遇 ZWJ、零宽字符、DEL、软连字符失败：只剔除 <0x20，`%q` 产出的 `\u200d`、`\x7f` AppleScript 不认（-2741），👨‍💻 类 emoji 都会触发；失败只记 Warn | `internal/sysnotify/sysnotify.go:49`、`:63` | 〔复现〕（osascript 字面量） |
| B23 | Windows 把通知 ID（`zakki-reminder-1-0`）当 AUMID 传给 `CreateToastNotifier`，系统 toast 基本不显示 | `internal/sysnotify/sysnotify.go:76`、`:92` | 〔静态〕 |
| B24 | Windows 命令注入面：PowerShell 把 U+2018–201B 当单引号，`escapeXML` 只转义 ASCII `'`；正文可来自同步进来的文件 | `internal/sysnotify/sysnotify.go:74`、`:81` | 〔静态〕，未在 Windows 运行 |
| B25 | 未命名标签通知正文以 ` · ` 开头（`tabTitle` 返回空串） | `internal/app/reminders.go:171` | 〔复现〕 |

### 1.3 存储

| # | 问题 | 出处 | 证据 |
|---|---|---|---|
| B26 | 「重新读取」后旧的失败重试覆盖外部内容（丢数据）：`Load` 只更新指纹，不清 `pending`、不停 `saveTimer`；保存失败会重挂同一份旧快照，重读后下一次重试通过校验写回旧内容（实测磁盘最终为 `LOCAL-STALE`，`EXTERNAL` 丢失） | `internal/notes/store.go:95-133`；`internal/notes/save.go:106-113` | 〔复现〕 |
| B27 | 超过 9 个标签被静默截断：移动端写入或 LWW 留档副本产生的第 10 个标签，桌面任意编辑一次后永久消失。另 `ValidTabColor` 为 nil 时清空所有颜色，M1 提取模型未注入校验器即触发 | `internal/notes/file.go:131-133`、`:148-160`；`internal/app/tab_model.go:25` | 〔复现〕 |

## 2. 设计缺陷

| # | 问题 | 出处 |
|---|---|---|
| D1 | 桌面不消费 `^`：带 `^` 的任务过锚点显示过期（违反 GLOSSARY「重复任务永不过期」），提醒只按锚点响一次，`~` 不作用于每个实例，勾选写 `[x]` 等于终止序列。spec 称桌面不在范围内，与 #12 定案「规格须写清桌面侧改动」及 ADR-0002:53 矛盾 | `internal/app/live_timechip.go:31`、`internal/app/reminders.go:105`；`docs/mobile-spec.md:15`；#12 |
| D2 | ADR-0002 自相矛盾且 core 无对应 API：「补勾只前进一步」后新锚点可能仍在过去，按「当前实例 = 不晚于当下的最近一个」派生，刚勾完的任务又显示未完成。core 只导出 `Expand`，无 `Current`/`Advance`；`^1y` 在 60 天地平线下拿不到下一时刻 | ADR-0002「完成推进」「过期自动跳」；`core/recur.go` |
| D3 | 「所有解析/写回经 core」不成立：保护区、块分类、通知正文投影在 internal；`AlertTiers` 返回切片无法 gobind（spec:65 却写「共用 core AlertTiers」）；#11 已出现 Swift 版 `parseRecur` 副本 bug，违反 engineering-standard 第 3 节 | `docs/mobile-spec.md:65-66`；`internal/app/reminders.go:116`；`internal/app/live_blocks.go`；#11 |
| D4 | ADR-0002「解析器带版本号」未实现；降级行为在 Find（忽略该段）、Split（整体不认）、bind（报错）三处不一致；`core/timetoken.go:16` 与 `:61` 注释互相冲突 | ADR-0002:24-25；`core/` |
| D5 | 偏移单位混用：令牌区间为字节，`protected` 为 rune 下标，Swift/Kotlin 用 UTF-16，中文正文三端对不齐 | `core/timetoken.go:231`；`core/parsejson.go:13-21` |
| D6 | Split 取行尾最后一个令牌，Find 取第一个；`splitKeepTimeSuffix` 仍用 Split，与 ADR-0001「第一个合法时间」不一致 | `internal/app/live_input.go:15` |
| D7 | bind 错误模型：「行内无令牌」是常态却返回 error；错误只有中文文案无错误码；白名单外 `alertMs`、非法 `recur`、`hasAlert=false` 但 `alertMs>0` 静默丢弃（违反「失败如实上报」）；`null` 报「due 非法」而非清除；`TimeTokenInfo` 与 `timeSuffixJSON` 两套形状，`alertMs` 一个 omitempty 一个不是 | `core/bind/bind.go:35-95`；`core/parsejson.go:13-21` |
| D8 | 错过即丢无补偿：无宽限窗口、无唤醒后对账、无「已错过」提示（B18/B19、启动时已过期） | `internal/app/reminders.go:112-114` |
| D9 | ADR-0001:8 写「时刻在未来即排定」，代码要求带 `~` 且按 `due-alert` 判断；照 ADR 实现的移动端会给无 `~` 的令牌发通知 | ADR-0001:8；`internal/app/reminders.go:105`、`:112` |
| D10 | 里程碑与「iOS 先行」矛盾：M3 出口要求双端到点响、§8 Android 精确闹钟验证在 M3，而 M1/M2 无 Android 工程；M1 要显示真实 zakki.json，同步 M4 才有，文件如何进设备未定义；M2/M3 期间手机编辑回不到桌面 | `docs/mobile-spec.md` §7–§8；#11 定案 |
| D11 | FCM 兜底层被悄悄丢掉：#1 决策摘要与 Android 报告定为三层含 FCM，spec §3.2、§5 均无，「不做」清单也未列 | #1、#10；`docs/reports/2026-10-27-android-notification-reliability.md:291-294` |
| D12 | Git 「接管」与 #1 Out of scope 冲突，地图未更新；后端 A 直接提交远端，本地 `autoCommit` 只 push 不 pull，必然 non-fast-forward | `docs/mobile-spec.md` §2.2；#1；`docs/reports/2026-10-27-zakki-sync-protocol-options.md:39` |
| D13 | LWW 欠规格：「按修改时间取新」用哪个时钟未定义，zakki.json 无修改时间字段；整文件粒度；留档副本复制 `@` 令牌导致重复通知；标签 ID 冲突；`active` 是设备态却在共享文件；与 9 标签上限冲突（B27） | `docs/mobile-spec.md` §2；#6 |
| D14 | 版本错位无策略：桌面遇高版本写保护，移动端强类型解码会丢未知字段再回写，未写如何保留未知字段与协同升版本 | `docs/architecture.md:134`；`docs/mobile-spec.md` |
| D15 | USE_EXACT_ALARM 排除依据是 Play 政策，但 spec 定 APK 侧载不上架，前提不成立，应重评（API 33+ 自动授予、不可撤销） | `docs/reports/2026-10-27-android-notification-reliability.md:59-72`；`docs/mobile-spec.md:31` |
| D16 | 原生重复触发器省配额缺前提：未来锚点会立即开始匹配提前触发；表达不了 `<结束日期`；提前量跨日/跨月对不上；已完成实例如何屏蔽未说 | `docs/mobile-spec.md:73` |
| D17 | 「点组件 = 完成并推进」与 GLOSSARY「推进仅限任务行」、桌面「点组件开弹层」冲突；非任务行/非重复令牌点击行为、手机端从哪编辑时间均未定义 | `docs/mobile-spec.md:97`；`docs/design-tokens-mobile-draft.md:16`；`GLOSSARY.md:42`；`docs/architecture.md:73` |
| D18 | Settings 整体进移动模型，含 `alwaysOnTop`、`hideOnEscape` 等桌面专属字段，移动端如何处理与回写未定义 | `docs/mobile-spec.md:39` |
| D19 | 开放点未进 §8 验证清单：OneDrive 国行可达性（#6）、16 KB 页大小（#9，侧载同样受影响）、精确闹钟被拒降级（#9）、profile 过期检测与「已排定/待同步」展示（#3） | `docs/mobile-spec.md` §8 |
| D20 | 弹层只改日期时「完成」仍禁用：`DateInput` 变化不设 `hasTime` | `internal/app/live_timepopover.go:150`（〔静态〕） |

## 3. 风险

| # | 风险 | 出处 |
|---|---|---|
| R1 | 睡眠与改时区：`time.AfterFunc` 的单调时钟不计睡眠，合盖期间提醒迟到；唤醒后先编辑则被当错过丢弃；`time.Local` 只在启动时读，换时区不更新（〔静态〕） | `internal/app/reminders.go:151-154` |
| R2 | Time Sensitive 在免费 Personal Team 下可能不可用（第三方构建记录引用 Xcode 报错「Personal development teams do not support Time Sensitive Notifications…」），而这是 spec 的主要缓解手段（〔网络〕需 Xcode 实测） | `docs/mobile-spec.md:75`、`:139`、`:157`；https://www.rizomorf.com/blog/dosbts-fork-to-testflight |
| R3 | `vendor/zakki/core` 与 `core/` 会漂移：桌面编译 vendor 副本，当前已有格式差异，`make check` 不做一致性检查；改 core 忘记 `make vendor` 时测试照样全绿 | `vendor/zakki/core/recur.go:13-16`；`Makefile` |
| R4 | 展开开销随锚点年龄线性增长，非任务行锚点永不推进（0001 年锚点 `^w@1-7` 253 ms，地平线 1 万年 660 ms）；地平线按实例时刻而非触发时刻判断，`~1d` 最后一天落到窗口外；区间为 `(now, now+h]` | `core/recur.go:109-209` |
| R5 | iOS：64 条 pending 无分配策略（公平性、为「请打开 App」留槽、将满预警）；「重签即清零」可能误判（覆盖安装通常保留数据，应与卸载重装分开实测）；免费账号每 7 天 10 个 App ID、每设备 3 个 App，App Group 与小组件进程加载 Go 运行时未验证 | `docs/reports/2026-10-27-ios-local-notification-reliability.md:73-79`；https://faq.altstore.io/altstore-classic/app-ids |
| R6 | Android：未写 13+ `POST_NOTIFICATIONS`；只监听 `BOOT_COMPLETED`，缺 `MY_PACKAGE_REPLACED`/`TIME_SET`/`TIMEZONE_CHANGED`；force-stop 后「触发后重注册」链断；`setAlarmClock` 显示闹钟图标；receiver 冷启动 Go 运行时耗时未评估 | `docs/mobile-spec.md` §3.2、§5 |
| R7 | 凭据：Device Flow 令牌存储位置未写（Keychain/Keystore），prefs 可能落明文；OAuth `repo` scope 可访问全部仓库，建议 GitHub App user token（可限定仓库、会过期） | `docs/mobile-spec.md` §2.2；https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps |
| R8 | 云盘后端：OneDrive/iCloud 替换文件触发 `verifyDisk` 写保护；iCloud 换成仅云端占位；「更换位置」拒绝已有同名目标，第二台桌面加入已同步文件夹被挡；「macOS=iCloud / Windows=OneDrive」表述误导；第三方 provider 不保证 NSFileVersion，免费账号开不了 iCloud capability | `internal/notes/store.go:157-176`；`internal/notes/relocate.go:62` |
| R9 | 多设备重复通知：多台在线同一提醒各响一次，留档副本再翻倍，spec 无去重策略也未明确接受 | `docs/mobile-spec.md` |
| R10 | 工程引用：`zakki/core` 不可 `go get`，spec「固定提交引用」做不到，只能子模块 + 路径 replace；gomobile 官方仍标 experimental | `core/go.mod`；`go.mod`；`docs/mobile-spec.md:28` |
| R11 | 桌面存储与 Git：自动推送不 pull，非快进后每 10 分钟失败且无法自愈；迁址依赖 `os.Link`，exFAT/FAT/部分网络盘必败；osascript/PowerShell 无超时，Windows 不隐藏控制台；关窗后 toast 被丢弃；`SaveNow` 在 stopped 状态返回 nil | `internal/notes/git_schedule.go:211`；`internal/notes/relocate.go:162`；`internal/sysnotify/sysnotify.go:64`、`:93`；`internal/notes/save.go:36` |
| R12 | 每次按键在主线程对全部标签全量 `collectReminders`，防抖只推迟安装定时器不推迟采集 | `internal/app/view.go:589` → `internal/app/reminders.go:99` |
| R13 | 行尾输入 `^w@` 可能触发桌面行尾 `@` 唤起时间弹层（〔静态〕） | `docs/architecture.md:73` |
| R14 | 流程：`a52be1f` 是 `refactor:` 提交，却把 `^` 段纳入令牌区间，属结构迁移夹带行为变更，违反 AGENTS.md 硬规则 | `a52be1f` |

## 4. 测试覆盖

覆盖率（`CGO_ENABLED=0`，vendor 模式）：markdown 85.4%，notes 86.1%，core 82.0%，core/bind 79.3%，sysnotify 0%。core 在 UTC、New_York、Lord_Howe、Santiago、Tehran、Apia 下均通过，但现有用例基本不涉及夏令时。

- **sysnotify**：无测试文件；`sanitizeAppleScript`、`escapeXML` 是纯函数。
- **断言过宽**：`TestExpandAndEdgeCases` 只查结果含 `[`；`TestCollectReminders:115-118` 的 `if e.alert==0` 永不成立；`TestFireReminderDelivers` 不断言 ID、Title；`TestTokenJSONRoundTrip` 手写毫秒值、不走 Split，漏掉 B2。
- **时序余量窄**：`TestReminderStopAndReplacementGates/replacement` 用 300 ms 对 250 ms 防抖；`TestRebuildRemindersEditedBody` 用 20 ms 判断未投递。测试时钟基准为 UTC、令牌按 `time.Local` 解析（`internal/app/main_test.go:39`）。
- **ADR-0002 规则**：每天、工作日、双周、结束日期、月末/闰日跳过、错过不补有测试；`@1,3,5`、`^3d`、`^2mo`、`^2y`、次数>1 与周几集互斥、未知规则当普通文本无测试；推进、当前实例、永不过期、`~` 作用于每个实例无实现也无测试。

建议补的用例：

| 函数 | 输入 | 期望 |
|---|---|---|
| `bind.SplitTimeSuffixJSON` → `JoinTimeSuffixJSON` | `x @2026-10-25 10:00 ~10m` | `alertMs=600000`，往返不变 |
| `app.applyTimePopover` | 行含 `^w@1-5`，只改时刻 | 写回保留 `^w@1-5` |
| `core.Expand` | `every` 0/-1 × d/w/mo/y；`weekdays:[0]`/`[8]`；every=2+weekdays；`^9999999999y` | 返回 error，2 s 内完成，不 panic |
| `core.Expand` | `maxCount` 0、1；结束日期早于当下；负地平线 | `[]`、恰好 1 个；空结果为 `[]` |
| `core.Expand` | `TZ=America/New_York`，锚点 `2026-03-07 02:30` 的 `^1d`、`^1w` | 03-09 起回到 02:30 |
| `core.Expand` | `^w@1,3,5`、`^3d`、`^2mo`（1/31）、`^2y`（2/29） | 实例正确，不存在日期跳过 |
| `core.parseRule` | `^w@1-5x`、`^1d<2025-13-45`、`^2w@1`、`^1mo@1`、`^0d`、`^w@`、`^1d<2025-1-1` | 全部 error |
| `core.FindTimeTokenEx` | `~10mins`、`~1hour`、`~0m5`、`@… ^1d ~10m` | 档位不部分识别；逆序不重复写回 |
| `ParseTimeToken` 对 `FindTimeTokenEx` 差分 | 复用 `rowTimeCases` + 转义、行内代码、`^hello` | 两者一致 |
| `core.SplitTimeSuffix` | New_York 下 `@2026-03-08 02:30` | 与 Find 一致 |
| `app.shutdownForQuit` | `storageMoving=true` 退出后再编辑 | 返回 error，之后仍能装定时器 |
| `app.deleteTab`/`renameTab`/`reloadNotes` | 被删标签带未来提醒；改名 | 旧代作废不投递；正文用新名 |
| `app.collectReminders` | 剩 2 h 设 `~1d`；`fireAt==now`；引用内 `[x]`；软换行令牌；带 `^` 的行 | 行为明确并有断言 |
| `app.connectReminders` | 预置笔记含未来提醒 | 首次调度后定时器数为 1（重启恢复） |
| `sysnotify.escapeXML`/`sanitizeAppleScript` | `<&'"`、`\n\t`、ZWJ emoji、弯引号 | 转义正确；Windows 脚本使用应用 AUMID |
| `notes.Store.Load` | 失败重试挂起时重读 | 不被旧快照覆盖 |
| `notes.Migrate` | 10 个标签；nil `ValidTabColor` | 不丢标签、不清颜色（或写保护） |
| `notes.flushPending` | `stateWrite` 先失败一次再成功 | 按 interval 自动重试成功 |
| `notes.writeExclusive` | 目标被抢先创建；`SyncDir` 失败；不支持 `Link` | EEXIST；候选文件清理；友好报错 |
| `notes.gitStep` | `rev-parse` 空串或仓库外路径；`status`/`push` 失败；非快进 | `GitStatus.Error` 对应，时间戳不更新 |

## 5. 建议修复顺序

1. **丢数据与崩溃**：B1（弹层保留 `Recur`）、B2（`/time.Millisecond`）、B3/B4（`Expand` 入口重校验、`Every` 上限、`mo`/`y` 按步跳、bind 层 `recover`）、B26（`Load` 成功清 `pending` 并停 `saveTimer`）、B27（超限写保护而非截断）、B16（退出取消后重连提醒）。
2. **收紧 core**：`ParseRule` 整段严格匹配并校验结束日期；档位后要求边界字符；`d`/`w` 从锚点按 k·Every 重建钟点；空结果返回 `[]`；删除 `parsejson.go` 正则实现改包装 `FindTimeTokenEx`，保护区下沉 core；增加显式时区注入（如 `SetLocation(tzName)`）；bind 包名改为 `zakkicore` 类专名，导出 `AlertTiers` JSON。
3. **桌面提醒生命周期**：B17 各入口改走 `rescheduleReminders`；`fireAt ∈ (now−宽限, now]` 立即投递；唤醒/激活时对账；sysnotify 改用参数传值（osascript `on run argv`、PowerShell `-EncodedCommand`）并使用固定 AUMID、加超时。
4. **规格与 ADR**：补桌面最小配套（重复任务不显示过期、勾选推进、按实例调度）或明确接受风险；core 增加 `Current`/`Advance` 与正文投影并在 ADR-0002 消解推进/当前实例矛盾；新 ADR 写清 LWW 时钟、留档副本是否剥离令牌、标签上限、未知字段保留与版本协同；FCM 去留；§8 补 Time Sensitive/App Group 实测、覆盖安装与卸载重装分测、16 KB 页、`POST_NOTIFICATIONS`、USE_EXACT_ALARM 重评、OneDrive 国行可达性。
5. **工程守护**：`make check` 增加 `core/` 与 `vendor/zakki/core` 非测试文件一致性检查；补第 4 节用例并在 `TZ=America/New_York` 下额外跑一轮 core。
