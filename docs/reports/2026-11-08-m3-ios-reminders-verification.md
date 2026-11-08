# 2026-11-08 移动端 M3-1 iOS 提醒验证记录

对应 [docs/mobile-spec.md](../mobile-spec.md) §7 里程碑 M3（实施票
[#51](https://github.com/gomeen/Zakki/issues/51)）：令牌弹层（D17）、通知对账重排（§3.3）、
完成推进（Advance）、64 槽与状态展示（§3.4）、Time Sensitive 尝试（R2）。模拟器端到端
实测完成；**真机项未验证**，清单见文末（不作为完成声明）。

## 第 0 步：core/bind 遗留 bug 核对（2026-10-28 评审清单）

逐项用临时探针程序核对（跑完删除，未入库），结论：**全部已在历史提交修复，本轮零 core 改动**，
`vendor/zakki/core` 副本 diff 一致（vendor-check），core 在 UTC 与 America/New_York 双时区全绿。

| 评审项 | 状态 | 证据 |
|---|---|---|
| B2 `alertMs` 纳秒/毫秒错 | 已修（9f926fe） | `bind.go` 输出 `/time.Millisecond`、读入 `* time.Millisecond`；`TestJSONRoundTrip` 走 Split 断言 `alertMs:600000`，Split→Join 往返不再静默删提醒 |
| B3 `Expand` 不校验 JSON | 已修 | `decodedRule→validateRule`：every 1..999、weekdays 升序去重 1..7、every>1+周几集拒绝；探针 every=0/-1×d/mo/y 均 2s 内返回不 panic |
| B4 超大间隔卡死 | 已修 | `^9999999999y`/`^100000000mo` 被 `ParseRule` 拒绝（次数上限 999 + 日历窗口扫描，不按锚点年龄迭代） |
| B11 `ParseTimeToken` 语义漂移 | 已修 | `parsejson.go` 已删除；绑定 `FindTimeToken` 包装 `core.FindTimeTokenEx` + UTF-16 保护区换算。探针：行内代码排除、保护区命中、非法日期继续找下一个、`^hello` 不报错 |
| B5 Android `time.Local` UTC | 不适用（本票 iOS） | 绑定面已有 `SetLocation`；iOS 由 `TimeZoneSync` 启动/时区变化注入，Android 留 M5 |
| B7/B8/B9/B10/B12/B15 | 已修 | 探针确认：maxCount≤0 与空结果返回 `[]`；`^w@1-5x` 等宽松规则拒绝；`~10min` 不部分识别；保护区 Replace 不改反引号内容 |

一处与评审时行为的差异值得记录：**非法 `^` 段（含未知规则）现在整簇不识别**（`FindTimeToken`
返回 not-found），比评审 B15 描述的「令牌仍成立」更严格——符合 ADR-0002「不触发」与规格 §3.3
「未知规则整簇不调度」，移动端采集口径据此直接跳过该类块。

## 规格条目 → 实现位置

| 规格条目 | 实现位置 |
|---|---|
| D17 点组件开弹层（任务/非任务行） | `TimePopoverCoordinator.openTimePopover`；焦点块 `BlockEditorView.pillTapped`、渲染块 `LiveTextCell.onTokenTap`（附件命中优先于进入编辑） |
| 弹层语义（预设+自定义重复、未改保留 `^` 原文、清除） | `TimeTokenDraft.swift`（桌面 `timePopoverState`/`timeRecurDraft` 同构移植：`token()` canonical 相等保留原文、custom 组装校验、invalid 保留原段） |
| 写回走既有管线并进撤销 | `TimePopoverCoordinator.applyLineEdit`（`recordOp(.command)` + remount + 焦点块同步缓冲）；行级替换经 `ZakkicoreReplaceTimeToken` + `MarkdownProtection` 保护区 |
| §3.3 对账主循环（同步留接缝） | `ReminderScheduler.attach/reconcile`：观察 snapshot 变化（250ms 防抖）、回前台、时区变化、到点投递；M4 同步成功后调 `reconcile()` 即复用 |
| 64 槽策略（最早 fireAt、平局稳定、最后槽续排、打开清理补足） | `ReminderPlanner.allocate`（63 + renewal，tie-break tabID→行→due）；`apply` 全撤全建 + `removeDelivered(续排)` |
| 完成推进（勾选即 Advance、ended 才完成、非任务不可推进） | `LiveEditorTransactions.toggleTask/advanceRecurringTask`：`ZakkicoreAdvance` 改写锚点（保留 ~ 与 ^ 原文段、保持未完成）；ended→标记完成；未知规则任务按普通任务翻转；渲染 cell 勾选不再要求焦点块 |
| §3.4 状态展示（不把写盘成功等同系统已接受） | `ReminderStatus` + `ReminderStatusBar`（列表页底部常驻）：已排定 N·下次时刻／待同步（`NotesModel.hasPendingWrites`）／待续排／权限受限（附设置入口） |
| Time Sensitive 尝试（R2/D19） | `content.interruptionLevel = .timeSensitive`（无 entitlement 由系统降级为普通通知）；`mobile/ios/Entitlements/Zakki.entitlements` 已备好**默认不接**（Personal Team 不支持该 capability，接入会破坏免费签名设备构建），付费团队真机验证时按 project.yml 注释启用 |
| 通知呈现（§3.4：可读正文投影、标题·正文） | `ReminderPlanner.notificationBody`（投影去令牌与标记、链接取显示文字、空白折叠，桌面 `reminderBody` 同构）；前台经 `willPresent` 呈现横幅+声音 |

## 自动化验证

- **iOS XCTest 251 条全绿**（iPhone 17 Pro 模拟器，iOS 26.5），本轮新增 53 条：
  - `ReminderPlannerTests`（12）：采集口径（`~` 必带、勾选排除、未知规则跳过）、重复展开（升序、间隔恰 1 天、时区钟面 ±1 容差）、64 槽（≤64 无续排、70 条 → 63+续排、平局稳定身份）、正文投影（去令牌/标记、软换行折叠）、Advance（下一实例/ended→nil/非法→nil）、due 格式化经绑定严格校验。
  - `TimeTokenDraftTests`（15）：播种与预设识别、`token()`「未改保留 `^` 原文」（非规范 `^1w@3,1<…` 原样回写）、custom 组装、every>1 丢周几集（桌面同构）、invalid 回空、suffix JSON 过绑定白名单、AlertTiers 形状与档位文案。
  - `ReminderSchedulerTests`（12）：spy 注入通知中心——对账排链、跨标签合并、70 条溢出加续排、打开清理已投递续排、denied 清空队列+受限态、provisional 视作已授权、待同步随未落盘翻转、`.timeSensitive` 尝试、触发器钳最小间隔、快照变化防抖重排。
  - `TaskAdvanceTests`（12）：容器级推进（改写锚点/保持未完成/撤销恢复/ended 完成/普通翻转/未知规则翻转/无焦点勾选）、弹层写回（改时刻保留段、清除、软换行段只改令牌行、行内代码保护区拒绝、焦点块缓冲同步）。
- **UI 端到端 4 条**（`ZakkiUITests`，独立 scheme `ZakkiUI` 本地跑，不进 CI）：点胶囊开弹层→完成回写、清除时间→源码模式验证 raw 无令牌、重复任务勾选→源码模式验证锚点推进且保持 `[ ]`、70 条任务行状态条显示「待续排」。
- **Go**：core 双时区全绿；`make check` 全绿（fmt/vet/race/arch/vendor，本轮无 core 改动）。

## 模拟器实测要点（iPhone 17 Pro，iOS 26.5，真实 App + 系统通知中心）

- **64 槽跨任务**：注入 3 标签 73+ 可调度实例（含 `^1d`/`^w@1-5` 重复展开），系统日志
  `ZAKKI-REM scheduled=63 renewal=1 authorized=1`——恰好 63 实例 + 1 续排通知，策略按规格生效。
- **到点响（前台）**：`willPresent id=zakki-reminder-…` 触发（os log），SpringBoard
  `UserNotificationsServer` 收到 `willPresentNotification` 响应；前台横幅呈现
  （模拟器灵动岛压缩样式，见截图 6）。
- **权限流**：首次启动正式授权弹窗→允许→`authorized=1`；provisional（e2e 静默路径）视作已授权；
  denied 分支清空待发队列并如实展示受限态（含跳系统设置入口）。
- **弹层与推进**：任务行/非任务行胶囊均可开弹层（截图 2/3）；勾选每日重复任务后胶囊从
  `2026-11-01 09:00 · 提醒 · 每天` 推进为 `2026-11-09 09:00 · 提醒 · 每天` 且保持 ☐（截图 4/5）。
- **过程中发现并修复**：`NSLog` C 变参直接传 Swift `Bool`/`String` 会产生垃圾输出甚至
  SIGSEGV（`objc_opt_respondsToSelector` via `os_log`，见 DiagnosticReports）——调度摘要改用
  显式 0/1 转换，willPresent 诊断改 `os.Logger`。

## 截图（release 资产 `m3-1-pr-assets`）

1. [标签列表 + 状态条（已排定 1 条 · 下次时刻）](https://github.com/gomeen/Zakki/releases/download/m3-1-pr-assets/m3-1-tablist-status.png)
2. [编辑器：任务行与非任务行时间胶囊](https://github.com/gomeen/Zakki/releases/download/m3-1-pr-assets/m3-1-editor-pills.png)
3. [时间弹层：时刻 / 提醒档位 / 重复预设 / 清除与完成](https://github.com/gomeen/Zakki/releases/download/m3-1-pr-assets/m3-1-popover.png)
4. [勾选推进后保持未完成（胶囊锚点已到明天）](https://github.com/gomeen/Zakki/releases/download/m3-1-pr-assets/m3-1-advance-live.png)
5. [源码模式验证推进写回（`^1d`/`~5m` 原文保留）](https://github.com/gomeen/Zakki/releases/download/m3-1-pr-assets/m3-1-advance-source.png)
6. [到点呈现瞬间（灵动岛压缩样式）+ 待续排状态条](https://github.com/gomeen/Zakki/releases/download/m3-1-pr-assets/m3-1-fire-moment.png)

## 真机待验项（未验证，不作为完成声明）

1. **Time Sensitive 实测**（R2/D19）：Personal Team 无 entitlement 的降级确认；付费团队接
   `Zakki.entitlements` 后验证 `.timeSensitive` 免打扰穿越与状态文案。
2. **到点响矩阵**：锁屏、后台、免打扰/专注模式、睡眠唤醒、手动改时、时区切换后的投递与对账。
3. **重签/重启分场景**（D19/R5）：免费账号 7 天重签流程、同 bundle 覆盖重签、覆盖安装、
   卸载重装分别核对数据／授权／pending 行为；App ID 每设备配额。
4. 状态条在真机节电模式下的刷新时机。

## 遗留风险与边界

- 状态条「下次触发时刻」在到点后、下次对账前短暂陈旧（已通过到点事件触发对账缓解）。
- UI 端到端测试不在 CI（依赖中文环境与坐标点按，稳定性不适合无人值守 runner），
  单元契约测试已覆盖同一语义。
- 段落块多令牌只识别首个（桌面同构口径）；通知正文取整块投影。
