# 调查报告归档

一次性性能、启动与渲染核查报告，带日期前缀，不作为日常维护的架构文档。
长期事实以 [architecture.md](../architecture.md) 为准。

| 报告 | 说明 |
|---|---|
| [2026-10-29-m2-editor-verification.md](2026-10-29-m2-editor-verification.md) | 移动端 M2 编辑器：子票 PR 对照、两次数据丢失级回归、126 条 XCTest 与性能预算 |
| [2026-10-28-m1-scaffold-verification.md](2026-10-28-m1-scaffold-verification.md) | 移动端 M1 iOS 脚手架：工程结构、导入管道与模拟器实测 |
| [2026-10-28-review-remediation-verification.md](2026-10-28-review-remediation-verification.md) | 评审整改的实现范围、自动化验证与原生复测结果 |
| [2026-10-28-reminders-mobile-review.md](2026-10-28-reminders-mobile-review.md) | 桌面提醒、共享内核 core、移动端规划与测试覆盖的全面审查发现 |
| [2026-10-22-review-remediation.md](2026-10-22-review-remediation.md) | 本轮 19 项代码审查问题的修复、回归证据与验证范围 |
| [2026-10-18-performance-review.md](2026-10-18-performance-review.md) | 窗口唤起、编辑/预览/撤销优化与剩余瓶颈 |
| [2026-10-18-startup-shutdown-review.md](2026-10-18-startup-shutdown-review.md) | 首帧后后台任务、启动/关窗原生实测 |
| [2026-10-18-focus-rendering-review.md](2026-10-18-focus-rendering-review.md) | 聚焦前后样式差异（已修复，保留根因记录） |

汇总数据见 [performance/summary.json](../performance/summary.json)；复现脚本见 [performance/](../performance/)。

## 仍未解决的性能项（摘自报告并按当前实现校正）

- **超长单段落布局**：代码块和表格已经具备块内行虚拟化，但没有空行的超长单段落仍需整体排版。
- **原生冷启动**：默认 Metal 冷启动的长尾仍需在目标硬件与正式签名包上持续复验。

旧报告保留当时的测量背景；其中已经完成的备份、目录迁移和 Git 超时整改不再列为当前问题。
