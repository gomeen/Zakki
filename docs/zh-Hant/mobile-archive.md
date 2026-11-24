> *本文為繁體中文（台灣）譯本，內容以[中文原文](../mobile-archive.md)為準，可能落後。*

# 行動用戶端封存：進度、未完成項與復原路徑

> **狀態：暫停，不再繼續開發（2026-11-11 決策，見 [ADR-0003](../adr/0003-pause-mobile-client.md)（中文））。**
> 目標規格文件已刪除（歷史版本見 git 歷史），程式碼仍在 `mobile/`，只凍結、不刪除。
> 本文回答四個問題：做到哪了、還差什麼、當初要做成什麼樣、將來想撿起來怎麼撿。

## 1. 為什麼停

- 只有免費 Personal Team 簽章：profile 7 天過期、每裝置 3 個 App ID、Time Sensitive
  entitlement 不可用（spec §10 D19，M3 出口的 A2 項因此無法驗證）。
- 手機上已安裝的 app 因簽章過期**已經不可用**，重新簽章流程對便箋這種高頻工具不可接受。
- 剩餘工作（§3.1 全部 11 項 + M5）**幾乎全在實機驗收**，沒有付費帳號就無法閉環。
  繼續寫程式碼只會得到一份自己用不上的實作。

## 2. 已完成（程式碼事實，非完成聲明）

### 2.1 里程碑

| 里程碑 | 交付內容 | 票號 | 驗證紀錄 |
|---|---|---|---|
| M1 腳手架 | 同倉 `mobile/binding`（獨立 module，`replace => ../../core`，唯一允許 `golang.org/x/mobile`）+ XcodeGen 專案 + `make mobile-framework` + `mobile.yml` CI + 檔案選擇器匯入 `zakki.json`；高版本寫入保護、未知欄位巢狀往返、超 9 分頁不丟資料 | [#25](https://github.com/gomeen/Zakki/pull/25) | [2026-10-28](../reports/2026-10-28-m1-scaffold-verification.md)（中文） |
| M2 編輯器 | SwiftUI 殼 + UIKit 文字堆疊雙模式（即時繪製 / 原始碼）、權杖簇往返與寫回、Markdown 快速列、最小主題集與防漂移同步、分頁管理綁定、系統分享匯出 | [#31](https://github.com/gomeen/Zakki/pull/31)–[#34](https://github.com/gomeen/Zakki/pull/34)、[#37](https://github.com/gomeen/Zakki/pull/37) | [2026-10-29](../reports/2026-10-29-m2-editor-verification.md)（中文）（126 條 XCTest）|
| M3 iOS 提醒 | 權杖彈層、單次通知重排、完成推進、重複展開、64 槽跨任務按最早 `fireAt` 分配 + 最後槽續排、狀態展示（已排定/待同步/待續排/權限受限降級）| [#51](https://github.com/gomeen/Zakki/issues/51) | [2026-11-08](../reports/2026-11-08-m3-ios-reminders-verification.md)（中文）（53 條新增 XCTest，模擬器 scheduled=63+renewal）|
| M4-1 同步引擎 + GitHub 後端 A | §2.2 判定表四種情況、sha CAS（409/422 重拉重判定）、Device Flow + 刷新 + 撤銷、Keychain、`conflicts/` 留存、衝突對話框 | [#52](https://github.com/gomeen/Zakki/issues/52) | [2026-11-08](../reports/2026-11-08-m4-1-sync-verification.md)（中文）（46 條新增 XCTest + 行程內 GitHub stub 端對端）|
| M4-2 iCloud 雲碟後端 B | 檔案選擇器授權 + security-scoped bookmark、基線=檔案身分 + mtime 的寫前比對、命名掃描 + NSFileVersion 雙通道衝突副本、書籤失效重選狀態機 | [#53](https://github.com/gomeen/Zakki/issues/53) | [2026-11-08](../reports/2026-11-08-m4-2-icloud-backend-verification.md)（中文）（35 條新增 XCTest + 3 場景 + 跨容器真實 picker）|
| iCloud 探針（D21 門禁）| 模擬器驗證了無 entitlement 的檔案選擇、書籤冷啟動復原和本機原地讀寫，以及佔位偵測與衝突掃描的本機行為；真實 iCloud Drive、佔位檔案下載和衝突偵測仍須實機驗證 | — | [2026-11-08](../reports/2026-11-08-icloud-sync-probe.md)（中文） |

### 2.2 M4-3 桌面同步配套：**屬於桌面，不隨行動端凍結**

這條容易被封存誤傷，單獨說明：同步偏好節（prefs v14）、舊 Git 排程器暫停/還原、
先拉後提交的同步迴圈，都是**桌面實作**，已隨 0.6.3 發布
（`f361012`，驗證紀錄 [2026-11-08](../reports/2026-11-08-m4-3-desktop-sync-verification.md)（中文））。
凍結的是 iOS 側同一套引擎，桌面側照常維護。

注意：桌面側後續已[用 ADR-0004 改掉了 D13 的衝突對話框與 `conflicts/` 留存](../adr/0004-sync-auto-merge-no-archives.md)（中文）
——雙方都改時按分頁自動合併，不再彈窗、不再留存。本文 §3.1 中帶「D13／留存／衝突對話框」
字樣的驗收項只適用於凍結的 iOS 側；桌面側對應項以 ADR-0004 為準。

### 2.3 程式碼與測試規模

| 位置 | 內容 | 規模 |
|---|---|---|
| `mobile/ios/Zakki` | SwiftUI 殼、編輯器、提醒、同步、設定 | 73 個 Swift 檔案 / 約 17340 行 |
| `mobile/ios/ZakkiTests` | 單元與契約測試 | 331 條 XCTest |
| `mobile/ios/ZakkiUITests` | UI 端對端（不進 CI，依賴中文環境與座標點按）| 10 條 XCTest |
| `mobile/binding` | gomobile 綁定建置 module | 2 個 Go 檔案（`deps.go`、`doc.go`）+ `go.mod` tool directive |
| `.github/workflows/mobile.yml` | 綁定產物 + 專案產生可重現 | 1 條 workflow |

## 3. 未完成

### 3.1 程式碼已完成、只差實機/人工驗證（11 項）

全部是原目標規格 §8 未勾選行（規格文件已刪除，歷史版本見 git 歷史）。按「復原時先做什麼」排序：

**第一優先（決定整條路線是否成立）**

| 項 | 內容 | 為什麼優先 |
|---|---|---|
| C1 | 實機 picker 裡 iCloud Drive 節點可見可選 | 失敗則 D21 規定後端 B 回落 M5，路線要改 |
| C4 | 佔位檔案下載輪詢（`notDownloaded` → `.current` → 讀全文）| `PlaceholderReader` 的佔位分支**從未執行過**，只編譯通過 |

**其次（提醒可用性）**

| 項 | 內容 |
|---|---|
| A1 | 免費 Personal Team 下 Time Sensitive 降級確認 |
| A2 | 付費帳號 + `Zakki.entitlements` 下的 Time Sensitive 免打擾穿越（**暫停的直接原因**）|
| A3 | 到點響矩陣：鎖屏 / 背景 / 專注模式 / 睡眠喚醒 / 手動改時 / 時區切換 |
| A4 | 免費帳號 7 天重新簽章流程（**暫停的直接原因**）|
| A5–A7 | 同 bundle 覆蓋重簽 / 覆蓋安裝 / 解除安裝後重裝，三種情況分別核對資料、授權、pending（不得互相推斷）|
| A8 | App ID 每裝置配額（每 7 天 10 個）|
| A9 | 低耗電模式下狀態列刷新時機 |
| A10 | profile 過期後的偵測與提示 |

**再次（同步）**

| 項 | 內容 |
|---|---|
| B1–B3 | 註冊 GitHub App → Device Flow 真實連線 → token 真實過期（8h）靜默刷新 → GitHub 側撤銷授權後斷線重連 |
| B4 | 行動網路實機 `api.github.com` 可達性 |
| B5 | 免費簽章覆蓋安裝/重新簽章後 Keychain 憑證存續 |
| B6 | 限流與大檔案（>1MB raw 回退）線上行為——目前僅 httptest mock |
| C2–C3 | 就地寫回 Mac 側可見、未授權直讀被拒 + 撤銷授權後報 `access-denied` |
| C5 | 真實衝突副本雙通道偵測（`zakki (N).json` / conflicted copy / NSFileVersion）|
| E1–E4 | **桌面 ↔ iPhone 雙向聯合測試**（M4 出口最後一環）、雙端真實 sha CAS 衝突、判定表四情況重現、舊 Git 排程器暫停還原（桌面側衝突已改為自動合併，聯合測試改為驗「合併結果可見、無雙寫」）|

**人工手感**（[#26](https://github.com/gomeen/Zakki/issues/26)）

匯入自己 `zakki.json` 的副本、長篇中文重度編輯（長文 + 多元件 + 中英混排 + 復原/剪貼簿/IME 組字）、
即時樣式過目（標題/粗斜體/任務/引用/程式碼區塊/時間元件 × 淺色 × 深色 × 橫向）、
寫入保護檔案保持唯讀、匯出 → 桌面再儲存後未知欄位不丟、橫向軟體鍵盤與拖曳排序手感。

### 3.2 完全未實作

| 範圍 | 內容 | 備註 |
|---|---|---|
| M5 Android | Android 專案 / Kotlin + Compose 編輯器 / AAR 建置與載入 / AlarmManager 鬧鐘（`setAlarmClock`、精確權限、Doze）/ BOOT_COMPLETED 等冷啟動重建 / 時區注入 / 雙端小工具 | `mobile/` 下無任何 Android 目錄，一行程式碼未寫 |
| M5 權限與生命週期 | Android 12+ 精確權限拒絕與降級、API 33+ `USE_EXACT_ALARM`、`POST_NOTIFICATIONS` 拒絕、通知通道關閉、force-stop 復原 | 同上 |
| M5 分發 | 簽章、分發流程、App Group / WidgetKit capability、16KB 頁大小 AAR | 同上 |
| M5 雲碟補齊 | OneDrive 中國版可達性、iCloud 與第三方 provider 差異、NSFileVersion 復原入口、第二台桌面加入既有檔案（spec R8）| 同上 |
| #57 | `ReminderFlowUITests.testPopoverClearRemovesToken` 在 main 上存量失敗 | 程式碼級問題，不是環境問題；復原時必須先修，否則 UI 測試基線是紅的 |
| #19 | Android FCM 兜底層研究 | 原本就延後到 v1 之後，隨行動端一起封存 |

## 4. 目標功能（原本要做成什麼樣）

規格文件已刪除，完整定義見 git 歷史中的 `docs/mobile-spec.md`。這裡只留索引：

| 想要的東西 | 規格位置 |
|---|---|
| v1 MVP 範圍：瀏覽、重度編輯（即時/原始碼）、分頁、設定與投遞提醒、重複全量、可插拔雙後端同步、今日到期小工具 | §5 |
| 五個里程碑的出口標準（M1 腳手架 → M5 Android 與收尾）| §7 |
| 雙後端同步協定（GitHub A / 雲碟 B）、衝突判定與留存（iOS 側；桌面側已改為自動合併，見 ADR-0004）| §2.2、§10 D13 |
| 時間與提醒的共享契約：綁定 API、排程主迴圈、通知呈現 | §3 |
| 已定決策：D11 無 FCM、D13 衝突不 LWW、D17 元件互動、D19 免費簽章邊界、D21 里程碑編排 | §10 |
| 視覺基線 | 原 `docs/design-tokens-mobile-draft.md`（已刪除，歷史版本見 git 歷史） |

**明確從未納入範圍的**（不是欠帳）：跨分頁搜尋、通知深鏈、應用程式內今日彙總頁（三項 fast-follow）、
iPad 專屬版面、手錶、Siri/捷徑、廠商推送通道、上架/TestFlight、堅果雲 WebDAV、
iCloud for Windows、CloudKit、Syncthing、自建同步服務、Dropbox（§0、§2.2 末行）。

## 5. 復原路徑

**前置條件（二選一，沒有第三條）**

- 開通付費 Apple Developer 帳號：Time Sensitive entitlement 可用、簽章一年有效；
- 或明確接受「7 天重新簽章、只自用、不承諾長期可用」，那麼 A2 與 A4 直接標為不適用，
  M3 出口降級為「重新簽章當天可用」。

**順序（不要跳步）**

1. `make mobile-framework` + `mobile.yml` 先跑綠，確認凍結期間 `core/` 的變動沒有破壞綁定編譯；
2. 實機驗 **C1 + C4**。這兩項決定 D21 的路線是否還成立；失敗就關 #53、後端 B 回落 M5；
3. 實機驗 A1、A3、A5–A10（A2 視帳號而定），M3 出口收尾；
4. 實機驗 B1–B6（GitHub App 註冊 + Device Flow + Keychain）；
5. 最後做 **E1–E4 雙端聯合測試**，這是 M4 出口的最後一環；
6. 然後才輪到 #26 人工手感與 M5。

**入口速查**

| 想找什麼 | 去哪 |
|---|---|
| 目標與出口標準 | 原 `docs/mobile-spec.md` §7、§8（已刪除，歷史版本見 git 歷史） |
| 各里程碑實作細節與證據 | `docs/reports/2026-10-2x-m1/m2…`、`2026-11-08-m3/m4-1/m4-2/m4-3…`、`2026-11-08-icloud-sync-probe.md` |
| 實機 runbook | `2026-11-08-icloud-sync-probe.md` 附錄 A |
| 綁定建置 | `Makefile` 的 `mobile-framework`、`mobile/binding/go.mod` |
| iOS 專案 | `mobile/ios/project.yml`（XcodeGen 產生 `.xcodeproj`，產生物不入庫）|

## 6. 凍結期間的約定

- 不修 iOS bug、不加 iOS 功能、不跑實機驗收；`mobile/` 與 `mobile.yml` 原樣保留。
- `core/` **不凍結**：桌面在消費它。改 `core/` 必須跑 `make test`（根目錄的 `go test ./...` 不執行巢狀模組的 core 契約測試），
  並注意 `mobile/binding` 依賴的 `zakkicore` 匯出面。
- 桌面側若改 `zakki.json` 格式或移轉語意，規格 §2.1 的共享契約仍受約束（ADR-0003 第 5 條）。
- issue [#26](https://github.com/gomeen/Zakki/issues/26)、[#50](https://github.com/gomeen/Zakki/issues/50)、
  [#57](https://github.com/gomeen/Zakki/issues/57)、[#19](https://github.com/gomeen/Zakki/issues/19)
  保持 open 作為復原入口，不進入 triage 佇列。

## 7. 凍結時的桌面待辦快照

下表保留凍結時的歷史紀錄，不代表目前進度。現行桌面待辦見 [`.scratch/`](../../.scratch/)，不改變行動端凍結範圍。

| 待辦 | 現況 | 涉及面 |
|---|---|---|
| [#22](https://github.com/gomeen/Zakki/issues/22) Windows 通知實機 + macOS 通知中心視覺確認 | 清單已寫好（`docs/verification/notifications-manual-checklist.md`），只有參數/腳本單元測試與交叉編譯證據 | 需一台實機 Windows + macOS |
| 桌面同步真實帳號驗證 | 桌面 GitHub 後端 A 已發布，但 Device Flow、token 過期/撤銷、限流、>1MB raw 回退仍只有 mock 證據 | 純桌面，1 個 GitHub App 即可開始 |
| Windows DPAPI 安全儲存 | 交叉編譯 + 介面測試過，**從未在真實 Windows 上執行** | 純桌面，需一台 Windows |
| Copilot 式輸入補全 | 調研報告已完成（[報告已收入儲存庫](../reports/2026-11-11-copilot-style-input-research.md)（中文）），MVP 建議在報告 §6.4；硬缺口是 MyGo 沒有 ghost text 裝飾層 | 純桌面，需先決策 |
| [#49](https://github.com/gomeen/Zakki/issues/49) Listed on Awesome MyGo | 對外清單登記，非程式碼 | 中繼資料 |
| 效能與啟動的後續建議 | 見 `docs/reports/2026-10-18-performance-review.md`、`2026-10-18-startup-shutdown-review.md` 的「後續建議」：大表格區塊內虛擬化、映像掃描與 Git runner 的逾時、備份同步入口背景化、移轉改中繼資料預檢 | 純桌面，均非阻塞 |
