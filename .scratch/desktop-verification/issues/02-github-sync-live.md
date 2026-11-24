# 02: 桌面 GitHub 同步真实账号验证

Status: ready-for-human
Blocked by: None

## 验收内容

在隔离的试验仓库、GitHub App 和临时笔记上验证当前桌面同步。[旧探针记录](../../../docs/reports/2026-11-08-m4-3-desktop-sync-verification.md)仅作线索；冲突行为以 [ADR-0004](../../../docs/adr/0004-sync-auto-merge-no-archives.md)为准，无需移动端联调。

- [ ] 真实账号完成 Device Flow、上传与拉取；断开后可重新授权。验证令牌过期、刷新与撤销的可见结果。
- [ ] 双端各改不同标签能合并，同标签冲突按当前规则处置，不弹二选一对话框或生成留档。
- [ ] 验证超过 1 MB 的笔记走 raw 读取回退；记录可安全复测的限流行为，无法实测时标明限制，不拿 mock 代替实测。
- [ ] 记录被测提交与逐项结果，不记录令牌、设备码、真实笔记或私有仓库详情。
