# 2026-11-08 桌面同步配套（M4-3）验证记录

对应 [docs/mobile-spec.md](../mobile-spec.md) §2.2 / §6.3 / §7 M4 行、实施票
[#54](https://github.com/gomeen/Zakki/issues/54)（总票 #50）：sync 偏好节（v14）、开启同步
暂停旧 Git 调度器、桌面同步循环（后端 A：先 pull 再 commit）、D13 冲突呈现与留档。
自动化 + 隔离探针实测完成；**真实 GitHub 连接、真机联调未验证**，清单见文末。

## 实现范围与位置

| 票面条目 | 实现位置 |
|---|---|
| sync 偏好节（v13→v14 迁移） | `internal/prefs/prefs.go`：`sync` 节（`enabled`/`backend`/`repo`，全部非凭据配置）；`enabled` 校验 backend=="github" 且 repo 为 owner/name；旧文件缺键解码即零值（关闭），无需迁移逻辑 |
| 暂停/恢复旧 Git 调度器 | `internal/app/prefs_runtime.go` `configureSync`（应用层按启用状态装配）；`internal/notes/sync.go` `ConfigureSync` + `armGitLocked` 的 `syncActive` 门禁（Store 兜底）；用户原开关从不改写（测试断言） |
| 同步循环（D12/D13） | `internal/notes/sync.go`：pull → 持锁本地阶段（flush 待保存 + verifyDisk + 判定表）→ CAS commit（期望版本号取本轮刚 pull 到的值，容忍同内容版本漂移）；CAS 拒绝重拉重判上限 3 轮；本地阶段持 `writeMu+gitGate`（与迁址、写保护互斥），网络不占锁 |
| 后端 A | `internal/notes/sync_github.go`：Contents API GET/PUT（blob sha CAS）、大文件 raw 回退、404 区分「文件缺失/仓库不可达」、409/422 归一为 `ErrSyncConflict` |
| 冲突呈现（D13） | 判定表 `decideSync`（纯函数，表驱动测试）；冲突挂起 → app 层 `syncPump` 弹原生对话框二选一（`internal/app/sync_bridge.go`）；设置页冲突区持久入口；落选版本写 `conflicts/zakki-YYYYMMDD-HHmmss-<设备名>.json`（与 zakki.json 同目录，规格原文命名）；留档列表/删除在 `settings_sync.go` |
| 基线（D13） | 应用数据目录 `sync-state.json`（设备本地；内容 sha256 + 后端版本号 + 本地/远端存在性）；损坏按「从未同步」保守降级；测试断言旧 Git 调度器只 add/commit zakki.json 精确路径，基线与 conflicts/ 不被收录 |
| GitHub 认证（桌面） | `internal/notes/sync_oauth.go` Device Flow（pending/slow_down/denied/expired + refresh 轮换）；`sync_keyring*.go` 系统安全存储（macOS `security` CLI 钥匙串 / Windows DPAPI 用户级加密 / 其他平台明确不可用）；`ZAKKI_GITHUB_CLIENT_ID` 注入 Client ID；`ZAKKI_GITHUB_BASE_URL` 仅供自测注入本地假服务（无设置入口，不进偏好） |
| 设置页 UI | `internal/app/settings_sync.go`（云同步分区：开关、仓库、连接/断开、立即同步、状态行、暂停说明横幅、冲突入口、留档列表）；文案全经 `locales.*`（en/zh-Hans 同步补键，catalog 再生成） |

## 自动化验证（make check 全绿：fmt/vet/mygo vet/test/race/arch/vendor）

- **prefs**：`TestPreferencesMigrateSyncSection`（v13 文件升级、写回带完整 sync 节、git 原开关保留）、
  `TestPreferencesSyncValidation`（开启缺后端/缺仓库/非法仓库形状/未知键/null 全拒绝）。
- **判定表**：`TestSyncDecisionTable` 13 条逐项覆盖 D13 四情况 + 首同步三态 + 远端丢失 + 裁决定向。
- **引擎**：`TestSyncAdoptRemote`（远端落盘、基线更新、`ErrSyncHold` 冻结、重载解除）、
  `TestSyncUploadLocal`（CAS 期望与内容）、`TestSyncBothChangedSameContent`（只更基线不上传）、
  `TestSyncConflictResolve`（两方向：留档内容=落选方、命名正则 `zakki-\d{8}-\d{6}-<设备名>.json`、
  胜者落盘与重载标记）、`TestSyncCASConflictRepull`（同内容漂移带新版本号上传；真分叉重判落入冲突）、
  `TestSyncCASExhausted`（恰好 3 轮后如实报错）、`TestSyncFirstSync`（空仓库上传/双方保守冲突）、
  `TestSyncFlushesPending`（待保存先落盘再上传）、`TestSyncSaveBlockedRefuses`（写保护拒绝且不触网推送）、
  `TestSyncRemoteMissingAfterBaseline`（远端被删按表采用=删本地）、`TestSyncPostSaveDebounceRepeats`。
- **后端 A / OAuth / 安全存储**：`sync_github_test.go` 10 条（httptest：pull base64/缺失/仓库不可达/
  大文件回退、push CAS 与 409/422 归一、401 文案、无 token 拒发、路径段转义；Device Flow 全状态机、
  刷新轮换回存、provider 过期刷新与过期无刷新报错、envelope 往返与空令牌拒绝）。
- **调度器暂停**：`TestSyncPauseGitScheduler`（1ms 周期下开启同步零 git 调用、关闭后恢复）；
  app 层 `TestSyncPauseKeepsGitPrefsAndRestores`（偏好原开关保留 + 横幅出现/消失 + 恢复）。
- **token 不落盘**：`TestSyncTokenNeverPersists`（同步全流程后扫描数据目录所有文件与 slog 缓冲，
  均不含 token；Authorization 头形状 `Bearer <token>` 由假传输层断言）。app 测试的安全存储注入
  内存实现（`newTestApp` 替换工厂），测试永不接触真实钥匙串。
- **UI**：`TestSyncSectionRendersAndValidates`（分区渲染、非法配置拒绝、Store 状态装配/复位）、
  `TestSyncConflictAreaButtons`（冲突行持久入口、保留远端点击、留档命名、删除）。

## 隔离探针实测（macOS，真实构建 + 本地假 GitHub）

探针：`.mygo/syncprobe`（临时入口，实测后删除）+ 本地假服务（Device Flow + Contents API，
逐请求 JSONL 证据）+ 隔离数据目录（含 git init 基线）。全程真实 UI 输入（orca computer），
截图与日志存 release 资产 `m4-3-pr-assets`：

1. **连接**：设置「保存与同步」→ 填仓库 `probe/notes` → 开启云同步 → 连接 GitHub…：
   用户码弹层展示 `PROBE-1234`（截图 01），假服务记录 `device_code_start → token_pending →
   token_granted`；状态行变「已开启」，按钮变「断开连接」。
2. **首轮上传**：远端缺失 → `pull_miss` → `push_ok`（提交信息含设备名），数据目录出现
   `sync-state.json`，状态行「上次同步：11-08 16:10」。
3. **暂停横幅**：开启 Git 自动提交后横幅出现（截图 02），且整个同步开启期间 git log 无新提交；
   用户开关值保持 1。
4. **远端安全跟随**：假服务推入远端新版本 → 「立即同步」→ 本地未变 → 采用远端：磁盘更新、
   界面重载、主窗口显示远端内容 + toast「已应用远端笔记」（截图 03）。
5. **冲突（D13 情况 4）**：假服务再推一版后本机再编辑 → 防抖轮触发 → 原生对话框
   「保留哪个版本？」三键（截图 04）→ 点「保留本机」：假服务 `pull_hit → push_ok`（以最新
   sha 覆盖远端），`conflicts/zakki-20261108-161524-gomeendeMacBook-Pro.local.json` 生成且
   内容与落选远端版本逐字节一致，本机获胜版本落盘；设置页冲突行消失（截图 05）。
6. **留档管理**：冲突留档区列出留档并逐条删除成功（目录清零）。
7. **凭据安全**：`security find-generic-password -s "Zakki Sync"` 探针期间存在、点「断开连接」
   后消失；数据目录全部文件扫描 0 处 token。
8. **关闭恢复**：关云同步 → 横幅消失、状态「云同步关闭」（截图 06）→ 再编辑一笔 → 约 1 分钟后
   git log 出现新提交 `notes: update zakki.json`（旧调度器按保留配置恢复）。
9. 探针正常退出（⌘Q 最终保存），进程清零。

## 实测发现并已修复的缺陷（均带回归测试）

1. **同步轮上下文提前取消**：pull 后立即 cancel，PUT 用已取消上下文报 `context canceled`；
   改为 pull 与 commit 共用本轮 ctx，本地阶段结束后才取消（`Close` 仍可经 `syncCancel` 中止）。
2. **防抖排程残留**：防抖定时器触发后字段未清零，后续保存永久不再装填同步轮；改为触发即清字段
   （`TestSyncPostSaveDebounceRepeats` 回归）。
3. **连接弹层阻塞轮询**：用户码 Dialog.Message 阻塞同 goroutine，授权轮询要等用户关弹层才开始；
   弹层移入独立 goroutine。

## 待验项（不作为完成声明）

- **真实 GitHub 连接**：真实 Client ID（GitHub App 注册）、真实 token 的 Device Flow、过期/刷新/
  撤销、API 速率限制与大文件（>1MB raw 回退）线上行为——全部仅 httptest mock 验证。
- **与 #52（iOS）联调**：桌面 ↔ iPhone 双向同步、CAS 冲突、移动端 Keychain 与桌面钥匙串互通，
  待 #52 合入后按总票 M4 出口联测。
- **Windows DPAPI 安全存储**：实现经接口测试与交叉编译，未在真实 Windows 上运行。
- **后端 B（iCloud 云盘）**：#53 范围，桌面侧未涉及；云端占位文件（iCloud 数据目录）未验证。
- macOS Keychain 实现为 `security` CLI 封装：argv 传参对本机进程可见是已知取舍（令牌不出本机），
  升级 UNUserNotificationCenter 式原生桥接属后续优化。
