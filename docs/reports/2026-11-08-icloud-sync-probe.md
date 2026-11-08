# Zakki iOS iCloud 可行性探针报告（同步后端 B：文件选择器 + security-scoped bookmark）

日期：2026-11-08 ｜ 环境：Xcode 26.5 (17F42)、iPhone 17 Pro 模拟器 (iOS 26.5)、xcodegen 2.46.0、macOS（已登录 iCloud，CloudDocs 存在）
探针工程归档：[icloud-probe-assets release](https://github.com/gomeen/Zakki/releases/tag/icloud-probe-assets)（源码 + 文本证据 + 本报告；解包后 `xcodegen generate` 重建）
决策上下文：[mobile-spec §7](../mobile-spec.md) D21（M3+M4 合并轮，后端 B 探针门禁）｜实施票 #53

## 0. 探针设计

- **Probe** App：7 个按钮（选择文件 / 授权目录 / 冷恢复书签 / 书签读写测试 / 无授权直读 / 占位状态检查 / 列冲突副本）+ 屏幕日志。**刻意不配任何 entitlements 文件**——所有 API 若能工作即证明不依赖 entitlement。
- **Seeder** App：往**自己的容器**写 `zakki-foreign.json`，为 Probe 提供「真跨容器」的安全作用域场景（比选自家文件更严格的验证）。
- **ProbeTests**（14 个单元测试）+ **ProbeUITests**（picker 端到端 + 3 个导航实验）。
- 签名证据：`codesign -d` 输出 `Signature=adhoc`、`TeamIdentifier=not set`、无 embedded entitlements（`Bundle.main.path(forResource:"embedded", ofType:"entitlements") == nil` 有测试断言）。

---

## 1. 问题一：文件选择器 + 书签就地读写，是否真的无需 entitlement？

### 结论：【已验证】（模拟器，机制层完整验证）——无需任何 entitlement

**端到端 UI 流程通过**（`test_ownFile_picker_bookmark_coldResume_write`，XCUITest 驱动跨进程 picker）：

```
Test Case '-[ProbeUITests.PickerUITests test_ownFile_picker_bookmark_coldResume_write]' passed
```

链路：UIDocumentPickerViewController（`forOpeningContentTypes:[.json,.text,.data], asCopy:false`）→ 用户点选文件 → `startAccessingSecurityScopedResource()=true` → 读 → `bookmarkData(options:.minimalBookmark)` 保存 → **terminate 进程重启** → `URL(resolvingBookmarkData:)` 冷解析成功 → 重新 `startAccessing` → 读 → counter+1 **就地写回** → 重新进入验证 `RESULT:WRITE-VERIFY-OK`。

**落盘硬证据**（macOS 宿主侧直接读模拟器容器文件，写入穿透了 App 容器边界真实落盘）：

```
$ cat <Probe容器>/Documents/probe-note.json
{ "app" : "Probe", "note" : "seeded by probe", "counter" : 3, "editedByProbe" : true }
```

counter=3 说明每轮 UI 测试的就地写入都真实持久化（同轮内写回+重进读回一致）。

**单元测试**（14/14 passed，摘录）：

```
test_bookmark_works_without_entitlements        passed   ← 无 entitlements 进程内书签创建/解析
test_bookmark_roundtrip_plain_file              passed
test_bookmark_resolve_is_stable_and_write_goes_through passed
test_startAccessing_on_own_container_file       passed   ← [T] startAccessing(self-container) = true
test_stale_bookmark_when_target_deleted         passed   ← [T] 目标已删：解析抛错 The file doesn't exist.
```

`codesign` 证据（构建产物无签名身份、无 entitlements）：

```
Signature=adhoc
TeamIdentifier=not set
（codesign -d --entitlements :- 无输出 = 无 entitlements 段）
```

### 【模拟器不可验·需真机】的部分

- **iCloud Drive 节点出现在 picker 中并选择 iCloud 文件**：模拟器未登录 iCloud（`FileManager.ubiquityIdentityToken=nil`，截图与日志均记录 `ubiquityToken: nil（模拟器未登录 iCloud）`）。picker 的 iCloud 节点依赖**设备登录 iCloud**，与 App entitlement 无关；需真机确认免费账号下节点可用。
- **跨容器「无授权直读被拒」断言**（`RESULT:NOACC-DENIED`）：测试代码与 App 按钮已就绪（`test_foreignFile_crossAppSecurityScope`），但 iOS 26.5 模拟器 picker 的子文件夹→根目录导航不工作（见 §4），无法自动到达 Seeder 文件夹；真机返回键正常时应可直接跑通。
- Apple 文档（Providing access to directories / FilePicker 相关）明确：document picker 的授权来自用户点选，app 出现在 设置→隐私→文件与文件夹，用户可随时撤销——`startAccessing`/读/写都要按会话失败处理，这与本探针的 API 用法一致。

### SDK 层面发现（对 M4-2 直接有用）

| 发现 | 证据 |
|---|---|
| `init(forOpeningContentAt:asCopy:)`（目录授权）**已从 iOS 26.5 SDK 移除**：编译报 `no exact matches in call to initializer`，Apple 文档页面 404 | 编译错误 + 文档抓取 Not Found |
| 现行目录授权方式：`UIDocumentPickerViewController(forOpeningContentTypes:[.folder])`，Apple 官方文档「Providing access to directories」的推荐写法；书签用 `.minimalBookmark` | 官方文档全文已核对 |
| `NSMetadataQueryUserAccessibleScope` / `NSMetadataQueryScope` 常量在 iOS SDK **不存在**（macOS CoreServices 未暴露到 iOS），`searchScopes` 用默认空数组即可 | 全 SDK 头文件与 swiftinterface grep 无结果 |
| `ubiquitousItemPercentDownloadedKey` 在 iOS 不可用，percent 走 `NSMetadataUbiquitousItemPercentDownloadedKey`（NSMetadataQuery 属性） | 编译错误 `is unavailable in iOS` |

---

## 2. 问题二：仅云端占位文件如何安全读取？

### 结论：【代码路径已编译未运行】（iCloud 真实分支）+【已验证】（本地行为边界与检测逻辑）

模拟器未登录 iCloud，**无法构造真实占位文件**，不臆断结果。完整代码路径已实现并编译（归档中 `Probe/PlaceholderReader.swift`）：

```
diagnose(): 读 NSURLIsUbiquitousItemKey / NSURLUbiquitousItemDownloadingStatusKey / IsDownloadingKey
readSafely():
  isUbiquitous=false → 本地快速路径直读
  status == .notDownloaded → FileManager.startDownloadingUbiquitousItem(at:)
                          → 轮询 status 直到 .current（0.5s 间隔，30s 超时）
                          → NSFileCoordinator.coordinate(readingItemAt:, options:[.withoutChanges]) 读
```

真机验证点（见附录 A runbook 第 4 步）：选 iCloud 占位文件 → 日志应显示 `downloadingStatus=notDownloaded` → 触发下载轮询 → `RESULT:PH-READ-OK`。

**【已验证】本地行为边界**（防误判 + 错误处理依据，单元测试输出）：

```
test_non_ubiquitous_file_has_no_downloading_status passed
  ← 本地文件 isUbiquitousItem=nil（键缺失，不是 false）、downloadingStatus=nil
    （检测逻辑用 `?? false` 兜底，不会把本地文件误判为占位文件）

test_startDownloadingUbiquitousItem_on_local_file_records_behavior passed
  ← [T] 对非 iCloud 本地文件调 startDownloadingUbiquitousItem = 抛错:
     "The file … couldn't be saved in the folder “tmp”."
    （必须 try+catch，不能裸调）

test_file_coordinator_read_local_baseline passed   ← NSFileCoordinator(.withoutChanges) 本地读基线
test_readSafely_local_fast_path passed             ← readSafely 本地直读原样返回
```

App 内实测（「占位状态检查」按钮对本地文件）：`[PH] 非 iCloud 文件 → 本地快速路径直读`、`RESULT:PH-READ-OK`；NSMetadataQuery 在未登录 iCloud 的模拟器返回 `结果 0 条`（`[MDQ]` 日志）。

---

## 3. 问题三：冲突副本枚举与 NSFileVersion

### 结论：【已验证】（副本命名扫描 + NSFileVersion 本地行为边界）+【需真机】（真实 iCloud 冲突）

**【已验证】副本命名扫描**（`ConflictScanner.scan`，单元测试 4 个断言全过）：

- `zakki (1).json` / `zakki (2).json` / `zakki (10).json` 识别为编号副本；不误伤 `zakki(1).json`（无空格）、`zakki (1).txt`（不同扩展名）、`zakki2 (1).json`（不同 base）。
- `zakki (Ringo's conflicted copy 2026-11-05).json`（iCloud 冲突命名风格，含大小写变体）识别为 conflicted 副本。
- 注意点（写进 M4-2）：扫描按文件名字典序返回，`(10)` 会排在 `(2)` 前面；Zakki 落地时应按编号数值排序后再挑「最新副本」。

**【已验证】NSFileVersion 行为边界**（iOS 可调用全部查询 API；行为实测）：

```
test_nsfileversion_on_plain_local_file_has_no_conflicts passed
  ← [T] currentVersionOfItem(plain local) = localizedName=…json（非 nil）
     otherVersions=0, unresolvedConflicts=0（普通本地文件无冲突，断言成立）
  ← [T] nonlocalVersions(plain local) = -1 err=NSFileProviderInternalErrorDomain 0
     "从URL …找不到有效的文件提供者。"（非 iCloud 文件调 getNonlocalVersions 会报错，需容错）

test_nsfileversion_on_missing_file_returns_nil passed  ← 文件不存在时 currentVersionOfItem=nil
```

**iOS 无法本地合成冲突版本**：`+addVersionOfItemAtURL:withContentsOfURL:` 头文件标注 `API_UNAVAILABLE(ios)`（仅 macOS）。因此「真实 iCloud 冲突副本出现时 `unresolvedConflictVersionsOfItem` 能否检出、冲突解决 UI 流程」**需真机验证**：两台登录同一 iCloud 的设备竞争写同一 `zakki.json` 触发冲突后，用探针「列冲突副本」按钮检查 `[CF]`（命名扫描）与 `[VER] unresolvedConflicts`。

---

## 4. 模拟器负面发现（XCUITest 驱动 picker 的实测经验，供后续复用）

1. **picker 内容由跨进程 DocumentService 加载**：Cancel 按钮先出现，内容渲染冷启动可达 15–30s，且偶发整页白屏；XCUITest 必须等「Cancel 存在 + staticTexts 数量增长」两重信号。
2. **跨进程元素可见性**：内容渲染完成后 XCUITest 可见（Cancel/浏览 tab/文件 Cell 均可查询点击）；渲染完成前完全不可见。
3. **文件 Cell 必须点 Cell 容器**；点内层 StaticText（文件名文本）不触发 `didPickDocumentsAt`（专项实验证实）。
4. **子文件夹返回根目录在 iOS 26.5 模拟器不可自动化**：返回键（BackButton「我的iPhone」）元素点击无效、左边缘滑动手势无效、`directoryURL` 指向其他 App 容器不生效 → `test_foreignFile_crossAppSecurityScope` 与 `test_dirGrant_directoryBookmark` 以 XCTSkip 留档（真机返回键正常即可跑通，代码完整保留）。
5. **剪贴板跨 App 读取会触发 iOS 16+ 隐私确认弹窗并阻塞主线程 30s**（曾导致 `main thread busy` 超时）——改为「Seeder 界面显示路径 → UI 测试读取 → `launchEnvironment` 注入 Probe」的无隐私通道。
6. 文本日志与测试记录已归档在 release 资产（截图/录像/xcresult 未归档以控制体积，报告 §1/§2/§3 引用的输出均出自归档内 `evidence/*.txt`）。

---

## 5. 对 M4-2（iCloud 后端）开工的判断输入

**可行性判断：B 路线（UIDocumentPicker + security-scoped bookmark）可行，且确认无需任何 entitlement，免费 Personal Team 可用。** 建议按此路线开工，但需接受其形态约束：

1. **授权是用户手动、一次性授文件夹/文件**，不是后台自动同步；每次启动流程 = 读书签 → 解析 → `startAccessing` → 用完 `stopAccessing`。Zakki 需要一个「选择 zakki.json 所在位置」的设置页 + 书签失效恢复 UI。
2. **占位文件读取必须走** status 检查 → `startDownloadingUbiquitousItem` → 轮询 → `NSFileCoordinator(.withoutChanges)`（`PlaceholderReader.readSafely` 可直接移植；阻塞轮询需改成异步）。
3. **错误处理清单**（实测行为）：
   - 目标文件被删：书签解析**抛错**（"The file doesn't exist."），不是 stale 标志 → 需引导用户重新选文件；
   - `startDownloadingUbiquitousItem` 对非法目标**会抛错**；
   - `getNonlocalVersionsOfItem` 对非 iCloud 文件**报 NSFileProviderError**；
   - 用户可在系统设置撤销授权 → 所有 `startAccessing`/读写路径都要按可失败设计。
4. **冲突策略**：文件名扫描（`base (N).ext` + `conflicted copy`，注意数值排序）+ `NSFileVersion.unresolvedConflictVersionsOfItem` 双通道检测；冲突解决 UI 是必做项（本探针只验证检测，不验证解决）。
5. **真机验收清单**（免费账号即可，见附录 A）：① iCloud Drive 节点在 picker 可见可选；② 就地写回后 Mac 侧可见；③ 无授权直读被拒（作用域负向）；④ 占位文件 status/下载轮询；⑤ 真实冲突副本的 `[CF]`+`[VER]` 检测。

真机 5 项随 #53 实施与 M3 真机验收窗口一并执行；若关键项失败（picker 无 iCloud 节点或占位读取不可用），按 D21 回落 M5。

---

## 附录 A：真机运行指南（免费 Personal Team，7 天 profile）

### 工程结构（见 release 资产 `zakki-icloud-probe.tar.gz`）

```
zakki-icloud-probe/
├── project.yml                  # xcodegen 配置（Probe 主探针 / Seeder 外部文件 App / 两组测试）
├── Probe/                       # SwiftUI 探针 App（7 个功能按钮 + 屏幕日志）
│   ├── ProbeApp.swift           # 入口 + UIDocumentPickerViewController 封装
│   ├── ContentView.swift        # 界面
│   ├── ProbeModel.swift         # 书签/读写/占位/冲突核心逻辑
│   ├── PlaceholderReader.swift  # Q2 占位文件安全读取（诊断→触发下载→轮询→协调读）
│   └── ConflictScanner.swift    # Q3 冲突副本命名扫描
├── Seeder/                      # 「外部 App」：往自己容器写 zakki-foreign.json（制造真跨容器场景）
├── ProbeTests/                  # 14 个单元测试（书签/占位/冲突，全部本地可验证部分）
├── ProbeUITests/                # XCUITest（picker 驱动 + 跨 App 流程 + 3 个导航实验）
├── evidence/                    # 文本测试日志
├── REPORT.md                    # 本报告原始版
└── RUN.md                       # 本附录原始版
```

### 模拟器复跑（本机已验证）

```bash
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
cd zakki-icloud-probe
xcodegen generate
xcrun simctl boot "iPhone 17 Pro"   # 已 boot 可跳过
# 全量（单元 14 个 + UI 6 个）
xcodebuild -project ZakkiICloudProbe.xcodeproj -scheme Probe \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro,OS=26.5' test
# 只跑单元测试
xcodebuild ... -only-testing:ProbeTests test
```

注意：`test_dirGrant_directoryBookmark` 与 `test_foreignFile_crossAppSecurityScope` 在 iOS 26.5 模拟器上会 XCTSkip（原因见 §4），属预期行为，不是回归。

### 真机运行（免费 Personal Team，7 天 profile）

1. Xcode 打开 `ZakkiICloudProbe.xcodeproj`。
2. 选 **Probe** target → Signing & Capabilities：勾选 *Automatically manage signing*；Team 选**个人免费团队**（登录 Apple ID 即有，无需付费开发者账号）；**不要添加任何 Capability**（不加 iCloud，这正是被验证的前提）。
3. iPhone 用数据线连接并信任；顶部设备选真机，Run。
4. 首次安装：手机 *设置 → 通用 → VPN 与设备管理* → 信任开发者证书。
5. **7 天过期**：免费账号 provisioning profile 7 天后失效，App 将无法启动；Xcode 重新 Run 一次即续签，App 数据不丢（除非卸载）。Seeder 同理。

### 真机测试 iCloud Drive（B 路线的关键验证点）

前置：iPhone 的 iCloud 账号与 Mac 相同，iCloud Drive 开启；Files app 里能看到「iCloud Drive」位置。

1. **就地打开 + 书签**：Mac 上把一个 `zakki.json` 拷进 iCloud Drive → 探针「选择文件」→ iCloud Drive → 选中 → 日志 `RESULT:BOOKMARK-SAVED`；杀掉 App 重开 →「冷恢复书签」→ 期待 `RESULT:COLD-RESOLVE-OK` + `RESULT:COLD-READ-OK`。
2. **就地写**：「书签读写测试」→ 期待 `RESULT:WRITE-VERIFY-OK`；到 Mac 上打开同一文件确认 counter 已变（跨设备就地写证据）。
3. **无授权直读（作用域负向验证）**：「无授权直读」→ 期待 `RESULT:NOACC-DENIED`（未调 startAccessing 时读 iCloud 文件必须失败）。
4. **占位文件**：Mac 上往 iCloud Drive 放一个较大文件（或 iPhone 断网后让另一设备上传新文件），iPhone 上选中它 →「占位状态检查」→ 期待 `downloadingStatus=notDownloaded` + 触发下载轮询 + 最终 `RESULT:PH-READ-OK`；同时观察 `[MDQ]` 行（NSMetadataQuery 的 status/percent）。
5. **冲突副本**：iPhone 与 Mac 各自快速修改同一个 `zakki.json` → iCloud 产生 `zakki (1).json` 或 `zakki (xxx's conflicted copy ...).json` → 探针「列冲突副本」→ 检查 `[CF]` 扫描结果与 `[VER] unresolvedConflicts` 是否 > 0。
6. 把以上每步截图 + 屏幕日志回填到本报告对应的「需真机验证」条目。
