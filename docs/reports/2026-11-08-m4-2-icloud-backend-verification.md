# 2026-11-08 移动端 M4-2 iCloud 云盘后端 B 验证记录

对应 [docs/mobile-spec.md](../mobile-spec.md) §2.2 后端 B「打开对账 + 冲突副本」模型、
§7 里程碑 M4（实施票 [#53](https://github.com/gomeen/Zakki/issues/53)，门禁探针见
[2026-11-08-icloud-sync-probe](2026-11-08-icloud-sync-probe.md)）：iCloud Drive 文件夹
文件选择器授权 + security-scoped bookmark、占位文件下载轮询、`zakki (N).json` /
conflicted copy 双通道冲突副本、复用 #52 的 D13 判定表与冲突对话框/conflicts/ 留档管线。
模拟器端到端实测完成（App 容器内 stub 文件系统 + 探针遗留跨容器真实 picker 授权）；
**真机 iCloud 项未验证**，清单见文末（不作为完成声明）。

## 规格条目 → 实现位置

| 规格条目 | 实现位置 |
|---|---|
| 后端 B = 云盘文件夹；以实际可访问文件提供方为准；打开对账（前台/聚焦），不承诺后台 | `CloudFolderBackend: SyncBackend`（pull/push/pushArchive 同一契约）；引擎前台时机复用 #52（回前台通知 + 设置页「立即同步」），无新增后台机制 |
| 认证 = 系统文件授权（用户手动一次性授予；可随时撤销；书签失效需重选） | `CloudFolderAccess`：`.minimalBookmark` 创建/解析 + `startAccessing`/`stopAccessing` 会话包装；书签 base64 进 Keychain（`TokenKey.cloudFolderBookmark`，R7 同 token 规则）；目标名/范围（文件夹/仅文件）进 UserDefaults |
| 基线 = 文件身份 + 修改时间（修改时间只作变化提示） | `CloudFolderAccess.version(of:)` = `fileIdentifier-mtimeMs`；每轮 `pullMain` 附带当前版本，D13 的「远端已变」由版本号驱动，内容是否真不同由 sha256 决定；写后重读返回新版本供基线记录（原子替换换身份、就地写变 mtime 均可检出——测试覆盖） |
| CAS：远端已前进不盲写 | `pushMain` 写前版本比对：新建语义要求文件不存在、更新语义要求版本一致，不一致抛 `casConflict` → 引擎重拉重判定（与后端 A 同一循环）；写前后固有小窗口由云盘冲突副本机制兜底（已接受的形态约束） |
| 冲突副本当作远端候选，走同一对话框（D13） | `CloudConflictEnumerator` 双通道：① `ConflictScanner` 命名扫描（`<base> (N).ext` 数值排序 + conflicted copy 大小写不敏感，误伤边界与探针单测一致）；② `NSFileVersion.unresolvedConflictVersionsOfItem`（非 iCloud 文件按空集容错，探针 §3）；副本经设置页列表/查看器「采用此版本」进入 `adoptCloudCopy` → 同一张 `ConflictResolutionCard`，`casBaseVersion` 取主文件当前版本（落盘目标是主文件），裁决后 `isResolved` 清版本标记（iOS 无 `resolve(byDeletingCurrentVersion:)`） |
| 落选版本完整留档 conflicts/，随同步保存 | 复用 #52 `ConflictArchiveStore`；文件夹授权把留档写进 zakki.json 同目录 `conflicts/`；仅文件授权写不进同目录 → `SyncError.archiveNotSupported`，引擎按「留档已在本机」收尾不再重试（设置页可见提示） |
| 占位文件（R8：占位不是可读文件） | `PlaceholderReader.read`（探针 §2 模板异步化移植）：诊断 `isUbiquitousItem/DownloadingStatus` → `startDownloadingUbiquitousItem`（try-catch，实测会抛）→ async 轮询 `.current`（可注入 sleep/超时）→ `NSFileCoordinator(.withoutChanges)` 读；本地文件走快速直读不误判（键缺失 `?? false` 兜底）。**占位分支本身需真机**（模拟器无法构造） |
| 后端切换：任一时刻只激活一个；换基线体系清基线 | `selectBackend` 切换即清空基线（A 的 blob sha 与 B 的身份+mtime 不可互比）；`connectCloudFolder` 重选位置同样清基线按「无基线」重新加入（测试覆盖：不清基线会把 B 位置误判为 adoptRemote） |
| 授权入口 + 书签失效恢复 UI | 设置页云盘节：未连接显示「选择 zakki.json 所在文件夹 / 仅选择 zakki.json 文件」两个 picker 入口 + 指导文案；同步报 `bookmark-invalid`/`access-denied` 时显示红色提示 + 「重新选择位置」（按原授权形态打开对应 picker）；连接态显示目标名/范围徽标/上次同步/立即同步/断开 |

## 自动化验证

- **iOS XCTest 331 条全绿**（iPhone 17 Pro 模拟器，iOS 26.5），本轮新增 35 条单测 + 4 条 UI 端到端：
  - `ConflictScannerTests`（5）：`(10)` 排 `(2)` 后（数值排序修正探针记录的字典序坑）、误伤边界（无空格/不同扩展名/不同 base/非数字编号/带尾缀）、conflicted copy 大小写变体、双通道合并不丢类、缺失目录空结果。
  - `CloudFolderBackendTests`（12）：pull 缺失→`.missing`、书签目标被删→`bookmarkInvalid`（探针 §5：抛错非 stale）、CAS 三态（新建遇已存在/版本不一致→`casConflict`；一致→写盘+新版本）、留档进云盘 `conflicts/`、仅文件授权→`archiveNotSupported`；引擎集成（真实临时文件系统走 `makeBackend()`，不经 mock）：无基线冲突→保留本机（主文件覆盖+基线+云盘留档一份）/保留远端（留档本地+安装）、外部改写→adoptRemote、本地已变远端未变→pushLocal、双方同内容→baselineOnly、**采用副本**（casBaseVersion=主文件版本≠副本版本，副本内容写回主文件）。
  - `CloudGrantFlowTests`（5）：连接持久化书签+强制选中后端 B+清基线、断开清书签与目标信息、**书签失效→重选状态机**（目标删除→`failed("bookmark-invalid")`→重选新位置恢复同步）、不可解析路径失败不产生半连接态、换位置清基线（不清会误判 adoptRemote）。
  - `PlaceholderReaderTests`（5）：本地文件不误判占位（verdict localFile）、本地快速直读、缺失文件语义化报错、版本号跟随身份+mtime（就地写与原子替换都前进版本）。
  - `CloudConflictEnumeratorTests`（5）：命名通道枚举（编号+conflicted、非副本不误伤）、无副本空列表（NSFileVersion 对普通本地文件空集容错）、仅文件授权跳过命名通道整体仍成功、失效书签 failure、版本标记清理空转不崩。
  - `CloudFolderFlowUITests`（4，`ZakkiUI` scheme 本地跑，不进 CI）：见下节。
- 顺带修复的引擎缺陷（新单测钉住）：**URL 实例会缓存 resourceValues**——`version(of:)`/诊断/轮询若复用同一 URL 对象会拿到旧 identity/mtime，基线判定会被陈旧版本号误导；统一改为每次新建 URL 对象读。

## 模拟器端到端要点（iPhone 17 Pro，iOS 26.5，真实 App）

启动参数 `-ZakkiSyncStub + -ZakkiCloudGrant <auto-folder|auto-file|missing>`（stub 专用）：
在 App 容器内临时目录重放种子文件并预建书签（自容器路径机制与真实 provider 一致，探针 §1），
不碰真实 iCloud；stub 每次启动重置后端选择与本地同步痕迹，避免场景间泄漏。

1. **文件夹授权全流程**（`testFolderGrantConflictCopiesAndArchive`）：连接态显示目标与「文件夹」
   徽标（截图 1）→ 云端冲突副本「查看副本（1）」（命名扫描通道，截图 2）→ 副本查看器全文 +
   「采用此版本」（截图 3）→ 立即同步：无基线双方不同 → 冲突卡片（复用 #52 同一张，截图 4）
   → 保留远端 → 状态「已采用远端内容」（截图 5）→ 本地落选版本完整入档（截图 6）。
2. **仅文件授权**（`testFileScopeArchiveLocalHint`）：目标行「仅文件」徽标 + 「冲突留档保留在本机」
   提示（截图 7）→ 冲突 → 保留远端 → 留档留在设备本地（截图 8）。
3. **书签失效→重选**（`testBookmarkInvalidShowsReselectHint`）：书签在、目标被删 → 立即同步如实报
   `同步失败（bookmark-invalid）` + 红色提示 + 「重新选择位置」入口（截图 9）。
4. **真实文件选择器授权**（`testPickerGrantFileRealFlow`）：未连接 → 切后端 → 授权入口 + 指导文案
   （截图 10）→ picker（跨进程，冷启动放宽超时）→ 浏览 → 我的 iPhone → **Probe 探针容器**（跨 App
   授权，比自容器更接近真机 iCloud 场景）→ 选 probe-note.json（截图 11）→ 连接态显示目标 +
   「仅文件」徽标（截图 12）。前置依赖 D21 探针 App（release icloud-probe-assets）提供可见容器，
   未安装则 XCTSkip；本 App 沙盒 **不带** `LSSupportsOpeningDocumentsInPlace/UIFileSharingEnabled`
   （避免同步目标可从文件 App 被外部改写的新故障面，属产品级取舍，未纳入本票）。
   过程发现：picker 跨进程会记住上次浏览位置（重开直接恢复在原目录），导航需按「文件是否可见」
   自适应；紧凑 sheet 无侧栏，位置切换走底部「最近项目/共享/浏览」tab。

## 截图（release 资产 `m4-2-pr-assets`）

1. [文件夹授权连接态（目标+范围徽标+副本入口）](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-connected.png)
2. [云端冲突副本列表（命名副本）](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-copies-list.png)
3. [副本查看器与采用入口](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-copy-viewer.png)
4. [D13 冲突卡片（后端 B 与后端 A 同一张）](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-conflict-card.png)
5. [保留远端后状态](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-kept-remote.png)
6. [conflicts/ 留档](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-archive.png)
7. [仅文件授权提示](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-file-scope.png)
8. [仅文件授权采纳后留档仍在本机](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-file-adopted.png)
9. [书签失效：失败码 + 重新选择位置](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-bookmark-invalid.png)
10. [未连接授权入口（引导选择）](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-pick-entry.png)
11. [真实文件选择器（我的 iPhone 容器列表）](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-picker-list.png)
12. [picker 授权完成（跨容器文件+仅文件徽标）](https://github.com/gomeen/Zakki/releases/download/m4-2-pr-assets/m4-2-cloud-picker-granted.png)

## 真机待验清单（免费 Personal Team 即可；探针报告附录 A runbook 同源）

模拟器未登录 iCloud（`ubiquityIdentityToken=nil`），以下项只能真机验证，本票**不臆断已验证**：

1. **picker 的 iCloud Drive 节点可见可选**：真机 Files/picker 出现 iCloud Drive 位置，可选中
   iCloud Drive 内的 zakki.json / 文件夹并完成授权（模拟器只能验自容器/跨容器路径）。
2. **就地写回跨设备可见**：iPhone 写 zakki.json 后 Mac 侧（同一 iCloud）立即可见新内容。
3. **无授权直读被拒**：未 startAccessing 直接读 iCloud 文件被拒；系统设置撤销授权后每轮同步
   如实报 `access-denied` 并引导重选。
4. **占位文件下载轮询**：仅云端占位文件 `downloadingStatus=notDownloaded` → 触发下载 → 轮询到
   `.current` → 读到全文（`PlaceholderReader` 的占位分支在模拟器不可构造，代码路径已编译未运行）。
5. **真实冲突副本双通道检测**：两设备竞争写产生 `zakki (N).json` / conflicted copy / NSFileVersion
   冲突后，设置页副本列表双通道都能列出；采用后主文件落盘、版本标记清除。

若关键项失败（picker 无 iCloud 节点或占位读取不可用），按 D21 回落 M5 并在票中注明证据。

## 遗留与风险

- 后端 B 的 CAS 是「写前版本比对」而非原子 CAS：写前后仍有小窗口，靠云盘自身冲突副本 +
  双通道检测兜底（规格已接受的形态约束，探针 §0/报告 §3 同结论）。
- 副本「采用」后命名副本文件本身不删除（保留现场、下次枚举仍会列出）；重复采用会走
  baselineOnly/无冲突路径，无数据风险。
- `NSFileVersion.unresolvedConflictVersionsOfItem` 的真实行为（含 replaceItem 提取内容）
  需真机确认；iOS 无本地合成冲突版本 API（探针 §3），单测只能覆盖空集容错。
- 占位轮询的超时（30s）与间隔（0.5s）为初值，真机视大文件下载表现再调。
- 存量问题不受本票影响：`ReminderFlowUITests.testPopoverClearRemovesToken` 在 main 上即失败
  （#57 承接）；本票 UI 测试未触及该路径。
