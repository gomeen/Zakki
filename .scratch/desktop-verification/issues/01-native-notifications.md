# 01: 桌面原生通知实机验收

Status: ready-for-human
Blocked by: None

## 验收内容

用当前桌面打包产物和隔离笔记数据完成剩余验证。以[手工清单](../../../docs/verification/notifications-manual-checklist.md)为准；[2026-11-19 的 macOS 记录](../../../docs/verification/notifications-2026-11-19/README.md)仅是历史证据。[原始事项 #22](https://github.com/gomeen/Zakki/issues/22)。

- [ ] 在 Windows 实机逐项执行 W1–W6，记录通知中心保留行为、图标、点击定位与去重；交叉编译不算通过。
- [ ] 在 macOS 用隔离的应用身份与测试数据验证 M2：退出后点击通知中心里的旧通知，应用冷启动并定位提醒。
- [ ] 验证 macOS M3：拒绝授权不走回落、应用内只提示一次、重新允许后恢复投递。
- [ ] 记录被测提交、系统版本、构建方式和逐项结果，在清单回链，不附真实笔记或账号信息。
