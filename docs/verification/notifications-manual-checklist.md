# 通知人工验证清单：Windows 实机 / macOS 通知中心

对应 issue「验证：Windows 通知实机与通知中心显示确认」。背景见
docs/reports/2026-10-28-review-remediation-verification.md：Windows 原生通知目前只有
参数/脚本单测、超时回归与交叉编译证据，缺实机验证；通知中心视觉确认同样缺失。
本清单供在真实设备上逐项执行并记录结果，完成后归档到 `docs/verification/`。

## 准备

- 被测构建：待验证 commit 的干净构建，记录 commit hash。
  - Windows：`GOOS=windows GOARCH=amd64 go build -trimpath -o zakki.exe .`
  - macOS：`go build -trimpath -o zakki .`
- Windows 实机一台（Windows 10 21H2+ / Windows 11），系统通知总开关开启，且未开启
  「专注助手/勿扰」拦截。
- macOS 实机一台：系统设置 > 通知 > Zakki，允许横幅与在通知中心显示。
- 触发方式：在笔记中写一条约 1 分钟后的提醒（内联时间令牌，见
  docs/adr/0001-inline-time-tokens.md），等待 reminders 触发通知。
- 相关实现：`internal/sysnotify/`（Windows 路径 `window_windows.go`：PowerShell +
  EncodedCommand + 固定 AUMID + 10s 超时）。

## Windows 实机验证点

### W1 toast 正常显示
- 步骤：双击 `zakki.exe` 启动，创建 1 分钟后的提醒并等待触发。
- 预期：桌面右下角弹出 toast，标题/正文与提醒一致；toast 过期后能在通知中心找到同一条。
- 结果：[ ] 通过 [ ] 失败（现象/截图：）

### W2 固定 AUMID 生效
- 步骤：分别以 1) 资源管理器双击、2) PowerShell `.\zakki.exe`、3) cmd 启动，各触发一次通知。
- 预期：三次通知来源均显示同一固定应用名，不随调用方式漂移；通知中心按同一应用分组。
- 结果：[ ] 通过 [ ] 失败（实际来源名：）

### W3 控制台不闪
- 步骤：从资源管理器双击启动（不经终端），触发通知，注视屏幕与任务栏。
- 预期：触发瞬间无黑色控制台窗口一闪而过（隐藏控制台路径生效）。
- 结果：[ ] 通过 [ ] 失败（现象：）

### W4 中文和 emoji 正文
- 步骤：创建提醒，标题用纯中文（如「会议纪要」），正文混排中文与 emoji
  （如「记得带伞 ☔️ ✅ 🎉」），触发通知。
- 预期：toast 上中文与 emoji 显示正确，无乱码、无「□」占位（EncodedCommand/UTF-16 编码路径生效）。
- 结果：[ ] 通过 [ ] 失败（现象：）

### W5 10 秒超时不阻塞主流程
- 步骤（二选一）：
  1. 临时构建：把通知脚本替换为 `Start-Sleep -Seconds 60` 后触发通知；
  2. 触发瞬间用 pssuspend / Process Explorer 挂起 powershell.exe（需多次尝试）。
- 预期：约 10 秒后通知调用按超时返回；期间与超时后应用 UI 均可正常编辑、保存，主流程不被阻塞。
- 结果：[ ] 通过 [ ] 失败（实际阻塞时长：）

## macOS 通知中心

### M1 通知中心实际显示效果
- 步骤：`./zakki` 启动，创建 1 分钟后的提醒并等待触发；横幅消失后打开通知中心。
- 预期：
  - 横幅：标题/正文排版正常，中文与 emoji 不乱码；
  - 通知中心列表：同一条通知出现，来源显示为 Zakki；
  - 点击行为：点击横幅或列表项，Zakki 窗口前置（若当前版本已实现点击行为）。
- 结果：[ ] 通过 [ ] 失败（现象/截图：）

## 环境与结果记录

| 项 | 值 |
|----|----|
| Windows 版本 / PowerShell 版本 | |
| macOS 版本 | |
| 构建 commit | |
| 执行人 / 日期 | |

| # | 验证点 | 结果 | 备注 |
|---|--------|------|------|
| W1 | toast 显示 | | |
| W2 | 固定 AUMID | | |
| W3 | 控制台不闪 | | |
| W4 | 中文/emoji | | |
| W5 | 10s 超时 | | |
| M1 | macOS 通知中心 | | |

完成后：截图与本文件（含结果）放入 `docs/verification/notifications-<日期>/`，并在
docs/reports/2026-10-28-review-remediation-verification.md 的「未验证项」处回链执行结果，
关闭本 issue。
