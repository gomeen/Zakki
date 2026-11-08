# 2026-11-08 移动端 M4-1 同步引擎与 GitHub 后端 A 验证记录

对应 [docs/mobile-spec.md](../mobile-spec.md) §2.2 同步协议、§7 里程碑 M4（实施票
[#52](https://github.com/gomeen/Zakki/issues/52)）：GitHub Contents API sha CAS 后端、
Device Flow 认证（GitHub App user token）、D13 冲突判定与二选一对话框、conflicts/ 留档、
前台拉推与 M3-1 对账管线串联。模拟器端到端实测完成（进程内 GitHub stub）；**真实 GitHub
账号与真机项未验证**，清单见文末（不作为完成声明）。

## 规格条目 → 实现位置

| 规格条目 | 实现位置 |
|---|---|
| 后端抽象（双后端任一时刻只激活一个，为 B 留位） | `SyncBackend` 协议（pullMain/pushMain(CAS)/pushArchive，version 语义随后端）；后端 B（云盘，#53）实现同一契约接入，设置页灰显占位（`SettingsView.row(.cloudFolder)`） |
| 每轮先 pull → 处理本地待写与冲突 → 再 commit；串行不混两条提交链（D12） | `SyncEngine.syncNow()`：flush 防抖待写 → 写保护拦截 → pull → D13 判定 → 动作 → 留档随轮上传 → 更新基线；`isSyncing` 门闩串行（并发触发只跑一轮，测试覆盖） |
| sha CAS：CAS 失败重新 pull 重判定，不盲重试旧 sha | `GitHubContentsBackend.put`：409/422 语义化为 `SyncError.casConflict` → 引擎循环重拉（上限 3 次，超过报 `cas-contention`），重试永远带新 pull 的 sha |
| 基线 = 设备本地上次成功内容 sha256 + 后端版本号；不进 zakki.json/prefs/Git（D13） | `SyncBaseline` + `SyncPersistedState` 持久化到 Application Support/zakki-sync-state.json（设备本地独立文件，不参与同步不上传） |
| D13 四情况判定（时钟无关，冲突按整个文件处理） | `ConflictDecision.evaluate`（纯函数注入 sha256）：四情况 + 无基线/远端缺失变体 + 写保护拦截；本地「已变」比较内容 sha256，远端「已变」比较后端版本号 |
| 冲突对话框二选一 | `ConflictResolutionCard`（root overlay，避免与设置 sheet 的 presentation 竞争）：保留本机 = 留档远端 + CAS 上传本地；保留远端 = 留档本地 + 安装远端；稍后处理 = 下一轮重新判定再弹 |
| conflicts/ 留档（不进标签、不参与调度、不占 9 标签；可查看/删除/不自动清理） | `ConflictArchiveStore`：`conflicts/zakki-YYYYMMDD-HHmmss-<设备名>.json`（同秒碰撞递增序号不覆盖）；本地与 zakki.json 同目录 + 随同步 PUT 到仓库 conflicts/ 路径（失败保持 pending 下轮重试）；设置页列表/查看/删除 |
| Device Flow（GitHub App user token；pending/slow_down/expired/denied） | `GitHubAuthService.startDeviceFlow/poll`（RFC 8628 语义；slow_down 间隔 +5s）；弹层展示 user_code + 授权链接（`DeviceFlowSheet`） |
| token 过期/刷新/撤销（§8 M4）；凭据只进 Keychain（R7） | `authorizedToken`（临期 <120s 走 refresh_token 刷新并轮换）/`forceRefreshAfterUnauthorized`（401 兜底强刷一次重试）；刷新失败 = 撤销语义：清凭据断连报 needsReauth；token/refresh_token/过期时刻只经 `TokenStoring` 进 Keychain（`KeychainStore`，service com.gomeen.zakki.ios.sync），不进 zakki.json/prefs/日志 |
| 同步时机：前台/聚焦拉推 | `SyncEngine.attach`：回前台通知触发 `syncNow()`；设置页「立即同步」手动入口 |
| 成功应用远端后立即全量对账提醒（串联点） | `SyncEngine.onRemoteAdopted` → `ReminderScheduler.shared.reconcile()`（M3-1 对账管线；ContentView.onAppear 装配） |
| 写保护禁止上传与自动覆盖（§2.1） | 引擎轮次首道拦截：`snapshot.writable == false` 整轮 `blockedWriteProtected`，零网络调用（测试覆盖） |
| active 字段原样往返；当前打开标签记设备本地 | 安装远端整文件字节原样替换（`installSyncedContent` → core Decode/Encode 保留未知字段与 active），引擎不改写字段；点开标签仍走设备本地导航（M1 语义） |
| client_id 可配置（真实账号连接为待验项） | 设置页输入 client_id / owner / repo（非凭据项进 UserDefaults suite）；GitHub App 注册步骤见文末待办 |

## 自动化验证

- **iOS XCTest 296 条全绿**（iPhone 17 Pro 模拟器，iOS 26.5），本轮新增 46 条单测 + 1 条 UI 端到端：
  - `SyncDecisionTests`（10）：D13 四情况逐条、双方未变 idle、无基线×远端已有（内容不同→冲突/相同→只更基线）、远端缺失回补/首传、写保护拦截让位于一切判定。
  - `GitHubContentsBackendTests`（6）：GET base64 折行解码 + sha、404/410→远端缺失、PUT 带 sha 与 message（请求形状断言）、409/422→casConflict、留档上传走 conflicts/ 路径且不带 sha、401→强刷重试成功。
  - `GitHubAuthServiceTests`（8）：Device Flow 起始解析、轮询状态机 pending→slow_down(+5s)→authorized、expired/denied 终态、未过期零网络、临期刷新并轮换 refresh_token、刷新被拒→断连清凭据、无 refresh token→needsReauth、断开清空全部凭据键。
  - `TokenStoreTests`（2）：mock 存储往返/删除 + 真实 Keychain 冒烟（唯一 service，测试后清理）。
  - `SyncEngineTests`（14）：首轮创建上传与基线、情况1 采用远端（active 原样往返、对账串联点触发）、情况2 CAS 基准=远端版本号、情况3 字节一致只更基线、情况4 保留本机（留档远端+pending 清除）/保留远端（留档本地+安装+对账）、CAS 拒绝重拉重判定不盲重试旧 sha、连续竞争安全阀、写保护零网络调用、防抖待写先冲刷再判定、needsReauth 呈现、并发触发串行一轮、留档 pending 失败重试、状态文件跨引擎实例持久化（基线生效不重复上传）。
  - `SyncFlowUITests`（1，`ZakkiUI` scheme 本地跑，不进 CI）：见下节。
- 传输全走 URLProtocol 脚本 mock / 进程内 stub，**不依赖真实 GitHub 与真实凭据**；Keychain 凭据测试用 mock 存储（真实 Keychain 仅冒烟并清理）。
- 模拟器跑测前 export DEVELOPER_DIR（xcrun 读该变量），destination 探测与 mobile.yml 相同。

## 模拟器端到端要点（iPhone 17 Pro，iOS 26.5，真实 App + 进程内 GitHub stub）

启动参数 `-ZakkiSyncStub` 接入进程内 URLProtocol stub（内存凭据、独立 defaults suite、
每次启动重置 stub 世界与本地同步痕迹），不碰真实网络与真实 Keychain。全流程
（`SyncFlowUITests.testConnectPullPushConflictAndArchive`）：

1. **连接**：设置页配置预填（stub）→ 连接 GitHub → Device Flow 弹层展示 `TEST-CODE` →
   stub 第一次轮询 pending、第二次授权 → 已连接（截图 2/3）。
2. **首轮拉推**：远端为空 → 创建上传 zakki.json，状态「已上传本地内容」，基线落盘（截图 4）。
3. **人为制造远端变化**：stub 在首个成功 PUT 后武装注入——下次 GET 前给远端追加标签；
   同时本机在源码模式键入未同步改动 → 第二轮 pull 得到双方已变且内容不同 → 冲突卡片
   （保留本机/保留远端/稍后处理，截图 5；卡片为 root overlay，设置页在冲突出现时自动收起
   避免 presentation 竞争）。
4. **保留远端**：本机版本完整留档 conflicts/ 并随轮上传 stub → 状态「已采用远端内容」、
   标签内容变更为远端版本（截图 6）。
5. **留档管理**：设置页冲突留档（1）→ 列表显示文件名/时刻/来源设备 → 查看全文（含落选的
   本机改动）（截图 7/8）。

## 截图（release 资产 `m4-1-pr-assets`）

1. [设置页同步节（后端选择 GitHub/云盘灰显、仓库配置）](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-settings.png)
2. [Device Flow：user_code + 授权链接 + 等待轮询](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-device-flow.png)
3. [已连接 GitHub（断开连接入口）](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-connected.png)
4. [首轮同步已上传本地内容](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-first-sync.png)
5. [冲突卡片：保留本机/保留远端/稍后处理](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-conflict-card.png)
6. [保留远端后状态「已采用远端内容」](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-kept-remote.png)
7. [冲突留档列表（文件名/时刻/来源设备）](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-archive-list.png)
8. [留档全文查看（落选版本完整内容）](https://github.com/gomeen/Zakki/releases/download/m4-1-pr-assets/m4-1-archive-content.png)

## 实现取舍记录

- **远端缺失处理**：D13 判定表不含远端缺失行；实现取「上传/回补」——本地永远有内容
  （首启种子），绝不清空本地迁就空远端；远端被外部删除时下一轮自动恢复。
- **冲突卡用 overlay 不用系统 alert**：同一视图树中 alert 与设置 sheet 的 presentation
  相互竞争（alert 挤掉 sheet 且不可复原）；overlay 无 presentation 语义且冲突出现时自动
  收起设置页，保证卡片置顶。
- **留档同名冲突**：同秒同设备留档递增序号后缀，不覆盖既有留档（完整留档优先）。
- **留档上传 409/422 视作已保存**：同名已存在于远端（前轮成功但确认丢失的极端情形），
  不无限重试。
- **已发现的存量问题（非本票引入）**：`ReminderFlowUITests.testPopoverClearRemovesToken`
  在 main 上即失败（stash 本票改动复测同样失败），属 M3-1 存量 UI 测试问题，不在本票范围。

## 待验项（未验证，不作为完成声明）

1. **GitHub App 注册与真实账号连接**（用户待办，票 #52 注明步骤）：在 GitHub 注册一个
   GitHub App（不需 Webhook，可 Request user authorization (OAuth) during install）→
   将 App 安装授权到存放 zakki.json 的仓库 → 在 iOS 设置页填入 App 的 Client ID 与
   owner/repo → 连接完成 Device Flow；预期验证 token 真实过期（8h）后的静默刷新与
   GitHub 侧撤销授权后的断连重连。
2. **蜂窝真机可达性**：api.github.com 直连桌面侧已实测通过，手机蜂窝网待真机复核（R9 关联）。
3. **Keychain 真机安全保存**（R7）：免费签名覆盖安装/重签后 Keychain 凭据存续行为。
4. **桌面 ↔ iPhone 双向联调**（与 #54）：桌面 sync 偏好节、旧 Git 调度器暂停/恢复、
   双端真实 sha CAS 冲突——桌面端同步配套落地后联调复现（M4 出口最后一环）。
