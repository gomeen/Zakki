# 系统通知改用 MyGo 原生通道，shell 投递退为回落

0.6.3 的通知是「应用内定时器到点 + shell 投递」（ADR-0001）：macOS 走 `osascript
display notification`，Windows 走固定 AUMID 的 PowerShell WinRT toast。两条路都只能
发一条文本，拿不到应用身份，也没有点击回调：

- macOS 的横幅来源是 Script Editor（osascript 的宿主），不是 Zakki，图标也不是应用图标；
- Windows 的来源固定为「Windows PowerShell」；
- 点通知不会唤起窗口，更不会回到对应的提醒条目。

MyGo v0.9.0（已在 `vendor/`）提供 `mygo.Notification`：macOS 走
`UNUserNotificationCenter`（应用包身份、前台也显示横幅、授权被拒可感知），Windows 走
托盘气泡（应用托盘图标）。原生点击回调通过 `App.OnNotificationClick(id)` 交给应用；
通知 ID 可以跨进程解析，但点击能否启动应用并交付 ID 取决于平台。

## 决策

1. **打包运行改用 MyGo 原生通知**。在主线程的 `App.WhenReady` 内用 `NotificationsSupported()`
   判定一次并缓存：可用就只走原生通道，不可用则交给 `internal/sysnotify`（包括裸 `go run`、`go test`）。
   就绪前到达的通知仅当次走回落，不缓存判定；选定原生后，投递失败或拒绝授权都不回落。
2. **通知 ID 承载定位信息**：`zakki-reminder/<标签持久ID>/<实例dueMs>/<行号>`。
   同一实例重发替换旧通知；点击按持久标签 ID（不是索引）切标签，行号命中就聚焦该块，
   行号因编辑漂移时按 due 兜底；标签已删除只唤起窗口；源码模式沿用会话选区，不抢光标。
3. **点击回调在 `App.Run` 之前注册**。macOS 由通知点击启动应用时，回调早于
   `WhenReady`；注册晚了会丢掉「点击启动」这一次。处理体再经 `WhenReady` 兜住窗口
   尚未就绪的情况。
4. **净化与截断沿用一套**：原生通道同样先过 `sysnotify.Sanitize`（控制符剔除，
   标题 100 / 正文 300 字符），长度上限与投递通道无关。
5. **未授权如实上报一次**：首次投递会触发系统授权询问，最多等 30 秒；被拒映射为
   `errNotifyDenied`，应用内提示一次「系统通知未允许」，该条提醒不重试（与其它投递
   失败一致）。
6. **仍不做 OS 级调度**：提醒依旧依赖应用存活，不引入触发器、分类或操作按钮；
   ADR-0001 的派生规则与补发宽限不变。

## Considered Options

- **osascript 加 `-sender` 冒充应用**：只能改来源名，仍是另一个进程的身份，
  图标、点击、授权状态都拿不到。拒绝。
- **为通知单独打包一个 helper .app**：多一个 bundle 与签名面，点击仍要跨进程转交，
  收益只是图标。拒绝。
- **在 `internal/sysnotify` 里手写 UNUserNotificationCenter（purego）**：等于把 MyGo
  已有的代理、授权、前台呈现再实现一遍，还会让「sysnotify 不依赖 MyGo」的分层失效，
  并且在 `CGO_ENABLED=0` 测试与交叉编译下要额外维护构建标签。拒绝。
- **Windows 继续用固定 AUMID 的 PowerShell toast**：它能留在通知中心，但没有应用
  图标与点击。为了与「真图标 + 点击跳转」的目标一致，改用 MyGo 托盘气泡，历史记录
  行为交回实机验证。

## Consequences

- 两平台的通知都显示 Zakki 自己的身份与图标；macOS 不再出现 Script Editor。
- 收到点击回调时，应用前置窗口，并按上面的规则定位提醒。macOS 的旧通知冷启动定位仍需实机验证；
  Windows 的托盘气泡依赖当次运行的托盘条目，不承诺退出后点击旧通知仍能定位。
- macOS 首次通知会弹出授权询问。若 30 秒内没有回答，该条投递作废（窗口开着时仍有
  应用内气泡），后续提醒不受影响。
- Windows 的「固定 AUMID / 来源显示 Windows PowerShell」验证项作废，清单已改写；
  Win10+ 把旧式气泡转成 toast 后是否留在通知中心、点击是否稳定投递，仍需实机确认
  （#22 保持 open）。
- 应用必须在运行：退出后不提醒，OS 级调度仍是后续增强。
- 本地去重键（标签 ID + 实例 due + 提前量 + 正文 hash，24 小时）不变，跨设备不承诺只响一次。
