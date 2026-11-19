# 通知人工验证结果：macOS（2026-11-19）

按 [notifications-manual-checklist.md](../notifications-manual-checklist.md) 执行，覆盖 ADR-0005
后的原生通道（MyGo / `UNUserNotificationCenter`）。本次覆盖 macOS 的 M1、M2 大部分与 M4；
M2 重启后点击、M3 与 Windows 未执行（原因见下）。

| 项 | 值 |
|----|----|
| 构建 | 工作树（ADR-0005 变更，未提交），`go tool mygo build -platform darwin/amd64 -skip-dmg` 产物 |
| 数据 | 隔离 `HOME` + 临时数据目录启动，未触碰真实笔记与偏好；提醒写在第二个标签 |
| macOS 版本 | 26.7.3 |
| 执行 / 日期 | agent 用 System Events 触发菜单、关闭窗口、删除标签与点击横幅；首轮点击由人工确认，2026-11-19 11:47–12:22 |

点击证据来自验证期间临时加的 `slog`（验证后已移除）：每条都记录
`id=zakki-reminder/<标签>/<due>/<行>`、`active`、`mdActive`、`source` 与 `winOpen`。

## 结果

| # | 验证点 | 结果 | 备注 |
|---|--------|------|------|
| M1 | 横幅：标题、正文、中文 | 通过 | 见 `banner.png`；图标为 Zakki 自己，标题「Zakki」，正文「提醒测试 · 带伞记得带伞」 |
| M1 | 横幅：中文 + emoji | 通过 | 见 `emoji-banner.png`；「带伞 ☔️ ✅ 🎉 中文与 emoji」渲染正常 |
| M1 | 图标与来源 = Zakki | 通过 | 见 `notification-center.png`；不再是 Script Editor |
| M1 | 通知中心出现同一条 | 通过 | 同上；按应用分组，正文一致 |
| M1 | 长正文截断（300 字符） | 通过 | 见 `truncated-banner.png`；`usernoted` 记录的正文为 301 runes（300 + 「…」），头部保留「提醒测试 · 」前缀 |
| M2 | 窗口在前台时点击 | 通过 | 前置窗口、切到「提醒测试」、`active=1`、`mdActive=1`（提醒块聚焦），见 `jump.png` |
| M2 | 窗口已关闭时点击 | 通过 | 点击后重开窗口、切标签并定位；`winOpen=true`、`active=1`、`mdActive=1`，见 `closed-window-jump.png` |
| M2 | 标签已删除时点击 | 通过 | 通知送达后删掉提醒所在标签再点击：窗口显示、`active=0`、`mdActive=-1`，不误跳 |
| M2 | 源码模式下点击 | 通过 | 先开「源码模式」再点击：`source=true`、`active=1`、`mdActive=-1`，只切标签不抢光标 |
| M2 | 应用退出后从通知中心点击 | 未验证 | 见「未执行项」 |
| M3 | 未授权提示与恢复 | 未验证 | 见「未执行项」；代码路径有单测（`errNotifyDenied`、只提示一次、不回落） |
| M4 | 退出后不提醒 | 通过 | 提醒前 4 秒 `kill -9`，过点 15 秒后 `usernoted` 无新记录（最新仍是 12:10 那条） |
| W1–W6 | Windows 实机 | 未验证 | 无 Windows 设备，代码与打包已交叉编译，见下 |

## 未执行项与原因

- **M2 应用退出后从通知中心点击**：点击会由 LaunchServices **冷启动已注册的应用**，
  继承真实 `HOME`，即跑真实笔记目录；为不碰用户数据，需要另建不同应用名/包名的探针包，
  并重新走一次系统授权提示。本次未做。
- **M3 未授权提示**：需要在「系统设置 → 通知 → Zakki」关闭再打开「允许通知」。
  自动化改系统设置未成功（AX 坐标与列表滚动不稳定），未冒险改偏好文件。人工执行约 2 分钟。

## 执行中确认的两点

1. 首次投递会触发系统授权（横幅式提示）。授权当轮的通知会进通知中心但不弹横幅
   （`usernoted` 记录的 `presented` 为 0），第二轮起正常；属首次授权的时序，不影响后续。
2. 窗口关闭时点击通知，应用先因激活而重建窗口（`OnActivate` 兜底），随后点击响应到达，
   最终停在提醒标签与提醒行——两条路径不冲突。
