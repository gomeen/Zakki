# 调查报告归档

一次性性能、启动与渲染核查报告，带日期前缀，不作为日常维护的架构文档。
长期事实以 [architecture.md](../architecture.md) 为准。

| 报告 | 说明 |
|---|---|
| [2026-11-08-m4-2-icloud-backend-verification.md](2026-11-08-m4-2-icloud-backend-verification.md) | 移动端 M4-2 iCloud 云盘后端 B：文件选择器授权/书签失效重选、身份+mtime 基线 CAS、双通道冲突副本、复用 D13 判定表与留档的 35 条新 XCTest 与模拟器端到端实测（含跨容器真实 picker 授权） |
| [2026-11-08-m4-3-desktop-sync-verification.md](2026-11-08-m4-3-desktop-sync-verification.md) | 桌面同步配套（M4-3）：sync 偏好节 v14、旧 Git 调度器暂停/恢复、先拉后提同步循环、D13 冲突对话框与留档，隔离探针全流程实测 |
| [2026-11-08-m4-1-sync-verification.md](2026-11-08-m4-1-sync-verification.md) | 移动端 M4-1 同步引擎与 GitHub 后端 A：D13 判定表、sha CAS 重拉、Device Flow/刷新/撤销、Keychain、conflicts/ 留档的 46 条 XCTest 与 stub 端到端实测 |
(docs: M4-3 桌面同步验证记录与架构/配置/规格勾选更新)
| [2026-11-08-m3-ios-reminders-verification.md](2026-11-08-m3-ios-reminders-verification.md) | 移动端 M3-1 iOS 提醒：评审遗留 bug 核对、弹层/对账/推进/64 槽实现对照、53 条新 XCTest 与模拟器端到端实测 |
| [2026-11-08-icloud-sync-probe.md](2026-11-08-icloud-sync-probe.md) | M4-2 门禁探针：文件选择器路线免 entitlement 验证可行，占位/冲突真机项与 runbook |
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
