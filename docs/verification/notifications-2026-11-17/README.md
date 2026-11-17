# 通知人工验证结果：macOS（2026-11-17）

按 [notifications-manual-checklist.md](../notifications-manual-checklist.md) 执行。本次只覆盖 macOS；
Windows W1–W5 无实机，挂起，#22 保持 open。

| 项 | 值 |
|----|----|
| macOS 版本 | 26.7.3 |
| 构建 commit | 2ae63b8（`go build` 隔离 probe：临时数据目录，未触碰真实笔记） |
| 执行 / 日期 | agent 执行，2026-11-17 |

## 结果

| # | 验证点 | 结果 | 备注 |
|---|--------|------|------|
| M1 横幅 | 标题与正文、中文与 emoji | 通过 | 令牌 `@2026-11-17 11:23 ~0m`，fireAt 11:23:00，横幅于 11:23:00–11:23:05 出现、约 5 秒后收起；标题「Zakki」，正文「通知验证 · 记得带伞 ☔️ ✅ 🎉 中文与 emoji 正文」，即「标签名 · 正文」投影，令牌已剥离，无乱码。见 `macos-banner.png` |
| M1 来源图标 | 宿主应用 | 符合预期 | 横幅图标为 Script Editor（osascript 宿主），与清单预期一致 |
| M1 通知中心列表 | 收起后在通知中心可见 | 未验证 | 本次未能截到通知中心列表，需人工补看 |
| M1 点击行为 | 点击不前置 Zakki | 未验证 | 本次未点击；按清单与 2026-11-17 调研，osascript 路线无点击回调 |

## 执行中发现的问题

1. **清单的触发示例写错，已修正。** 原文建议 `~1m`，但提前量白名单只有
   `0m/5m/10m/30m/1h/1d`（`core/timetoken.go` 的 `alertTiers`），`~1m` 使整个令牌失效、按普通
   文本处理，照做不会出通知。清单已改为白名单说明并推荐 `~0m`。
2. **probe 的数据目录是 `Zakki Dev`。** `go build` 出的 probe 走开发模式（`mygo.IsDev`），
   数据目录为 `<PathAppData>/Zakki Dev`，不是 `<PathAppData>/Zakki`。把 `zakki.json` 放进
   `Zakki` 子目录会触发首启向导。#26 评论里的 probe 步骤同样受影响（移动端冻结，不改）。
