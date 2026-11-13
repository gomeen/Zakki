# 暂停移动客户端开发，保留代码、规格与验证记录

Zakki 是单人维护的桌面便签应用，移动端的目的是「在 iPhone 上随时能看能改，到点提醒」。
按 [mobile-spec.md](../mobile-spec.md) §7 的里程碑，M1–M4 的代码已全部落地（iOS 双模式编辑器、
提醒调度、GitHub 后端 A、iCloud 云盘后端 B，桌面同步配套随 0.6.3 发布），但 §8 出口清单里
仍有 11 项是**手机真机与人工验收**，且其中多项的前置条件是开发者账号能力。

现只有免费 Personal Team 签名：profile 7 天过期、每设备 3 个 App ID、Time Sensitive
entitlement 不可用。实际结果是手机上的 app 已因签名过期不可用，而便签是高频刚需，
7 天一次的重新签名流程不可接受。继续投入 M5（Android）与真机验收，只能得到一份
**无法长期交付给自己使用**的代码。我们决定：**暂停移动客户端开发**，不推进 M5；
`mobile/` 代码、`docs/mobile-spec.md` 规格与全部验证报告原样保留并标记冻结。

## 决策

1. 移动端进入**冻结**状态：不修 iOS bug、不加功能、不做真机验收；`mobile/` 与
   `.github/workflows/mobile.yml` 原样保留，不删除、不迁分支。
2. 目标规格不删：[mobile-spec.md](../mobile-spec.md) 仍是未实现目标的唯一事实来源，
   §8 未勾项由「未完成」改述为「暂停待验」，出口标准不变。
3. 归档进度与恢复路径写入 [mobile-archive.md](../mobile-archive.md)，ADR 只记决策与后果。
4. `core/`（嵌套 module `zakki/core`）**不冻结**：桌面 `internal/notes`、`internal/markdown`
   正在消费它的时间令牌、重复展开与磁盘模型，桌面改动照常需要过 core 契约测试。
5. 运行时数据格式不变：`zakki.json` 版本 3 与「未知字段原样往返」的写入保护继续有效，
   冻结不构成迁移或放宽约束的理由。

## Considered Options

- **继续用免费账号自用（每 7 天重签）**：拒绝为当前选择。便签的高频使用场景要求随时可用，
  7 天一次的 Xcode 重装流程把维护成本转成了使用成本；实际观测到的结果就是 app 已经不可用。
- **转 Android 优先**：拒绝。Android 不需要付费账号即可侧载长期可用，但仍要一整套工程、
  双端小组件与分发维护；在桌面端仍有未收口的验证与功能时，把单点维护预算挪到第二平台
  不划算。若未来确实需要，另开 ADR 重新决策，不复用本决策的结论。
- **删除移动端代码与规格**：拒绝。约 1.7 万行 Swift + 340 条 XCTest + 20 份验证报告是资产，
  保留成本仅为一段冻结说明与一条 CI；删除后恢复要重新踩一遍 iOS 通知配额、书签、
  冲突副本这些只有真机才能暴露的坑。
- **只保留规格、删除实现**：拒绝，理由同上；且实现是唯一能证明规格可落地的证据。

## Consequences

- 移动端不再有「完成」的路径，[#26](https://github.com/gomeen/Zakki/issues/26)、
  [#50](https://github.com/gomeen/Zakki/issues/50)、[#57](https://github.com/gomeen/Zakki/issues/57)
  保持 open 并注明暂停，作为恢复时的入口，不再进入 triage 队列。
- `mobile/binding` 依赖 `zakki/core` 的接口；冻结期间若 core 改动破坏绑定编译，只会在
  `mobile.yml` 里暴露。可以接受：core 的破坏性改动本来就要单独评审，且恢复前必须先让
  binding 重新编译通过。
- `mobile.yml` 继续跑 macOS runner 的 `make mobile-framework`。若长期无人看管导致
  CI 噪音过大，可以单独禁用该 workflow——这属于运维取舍，不在本 ADR 内决定。
- `docs/mobile-spec.md` §2.2 的双后端同步契约中，**后端 A（GitHub Contents API）已由桌面
  单独实装并使用**，冻结只影响 iOS 侧的同一实现，不影响桌面同步。
- 恢复的前置条件有二选一：开通付费 Apple Developer（Time Sensitive + 长期签名），
  或明确接受 7 天重签流程且只做自用。恢复顺序见
  [mobile-archive.md](../mobile-archive.md) 第 5 节：先验 iCloud 真机两项，再按
  M3 → M4-1 → 联调 推进，M5 最后。
