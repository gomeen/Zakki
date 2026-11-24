> *本文為繁體中文（台灣）譯本，內容以[中文原文](../architecture.md)為準，可能落後。*

# Zakki 架構說明

面向後續維護者與 agent：先讀本文再改程式碼。本文只描述穩定事實，具體行為以原始碼為準
（關鍵處標註了文件與函式）。專案是 [MyGo](https://github.com/egoist/mygo) 原生 Go UI
桌面應用程式，無 HTML/JS/webview，純 Go 單一二進位檔。

專案分兩層：`internal/app` 是 UI 與應用程式裝配（唯一允許依賴 MyGo 的 internal 套件）；
`internal/` 其餘套件是純邏輯與持久化，禁止依賴 MyGo 與 `internal/app`。根目錄只有進入點 `main.go`。

依賴透過標準 `vendor/` 固定。MyGo 版本見 [`go.mod`](../../go.mod)，原生文字區間樣式補丁在
`patches/mygo-text-range-background.patch`，行內物件尺寸與真實幾何補丁在
`patches/mygo-textinput-objects.patch`，快速鍵優先於編輯器詞間移動的按鍵路由補丁在
`patches/mygo-shortcut-claimed-keys.patch`（⌘數字切分頁後編輯器持有焦點，⌘⌥←/→
仍能循環切分頁），日期面板欄寬與行間距補丁在 `patches/mygo-calendar-spacing.patch`；刷新依賴使用 `make vendor`，維護約束見
[依賴補丁](../../patches/README.md)（中文）。字型／斜體／字重進入原生版面，底色／顏色／底線僅參與繪製；
區間合併、IME、選取區與捲動仍沿用輸入控制項，不另外建立文字版面。

## 模組職責

| 檔案 | 職責 |
|---|---|
| `main.go` / `internal/app/lifecycle.go` | 進入點 `app.Run()`：單一實例、紀錄（只寫 stderr，不落盤）、啟動清掃崩潰殘留暫存檔（`temp_sweep.go`）、macOS 選單（應用程式/段落/格式/編輯/顯示/視窗）、系統匣、全域快速鍵、結束與更新收尾；主視窗 mac 為無邊框自繪頂端列，Windows 用隱藏標題列模式（`TitleBarStyle: hidden`）保留原生最小化/最大化/關閉（`StateKey: "main"`）、關窗常駐系統匣、`OnActivate` 重用 `showWindow`。選單項目與快速鍵細節見 `menu_commands.go`、`menu_edit.go` 與 [configuration.md](configuration.md)。 |

| `internal/app/updates.go` | 註冊 MyGo `plugins/updater/native` 原生更新視窗（跟隨介面語言、預設手動檢查）；應用程式選單與系統匣提供「檢查更新…」。`finishQuit` 讓更新重新啟動也經過最終儲存，失敗則取消結束。更新公鑰與 `gomeen/Zakki` 分發位址在 `mygo.json`；tag 經全量檢查後分別建置 macOS universal 與 Windows x64，雙平台均成功再彙總到草稿 Release；流程在 `.github/workflows/release.yml`，操作見 [release.md](release.md)。 |
| `internal/app/prefs_runtime.go` / `internal/app/prefs_bridge.go` | 偏好設定執行期整合：`loadPreferences`/`updatePreferences`；寫入磁碟委託 `prefs.Store`。型別別名在 `prefs_bridge.go`。 |
| `internal/app/notes_bridge.go` | 筆記磁碟型別別名（`settings`/`savedTab`/`savedFile` 等舊設定別名同在此檔案）與 `migrateSavedFile`、`newNotesStore`。 |
| `internal/app/storage_file.go` / `internal/app/settings_storage.go` | 唯一筆記檔案：`validateStorageDir`（目錄可寫探測）、`chooseStorageDirAsync`（背景彈目錄選擇器，經視窗 `Update` 回主執行緒）；`applyStorageDir` 執行期安全遷址——串行 `Relocate`（拒絕覆蓋既有目標 → 寫入並驗證候選 → 偏好設定持久化成功才切換權威路徑 → 刪除舊檔案，失敗保留原檔並如實報錯，同一路徑為 no-op）；「儲存與同步」頁展示檔案路徑、儲存狀態與 Git 開關。首次啟動精靈與設定頁共用。
| `internal/app/onboarding.go` / `internal/app/onboarding_view.go` | 首次啟動初始化精靈（僅全新安裝，`firstRun` = 資料檔案不存在）：640×560 獨立視窗（`StateKey: "onboarding-v2"`），兩頁草稿式確認——① 語言（English/日本語/简体中文/繁體中文，選擇即時 `App.SetLocale` 並寫入偏好）、明暗、風格（標準/手帳，寫入 `appearance.style`，不強制字型）、中文字型、英文字型（預選 JetBrains Mono）；② 初始分頁數 1–9 與筆記儲存位置（預填目前資料目錄，可更換，始終可見；完成後 `Relocate` 到所選資料夾的 `zakki.json` 並持久化 `storage.dataFile`）。選擇不觸發下載；主按鈕顯示「完成設定」或「下載字型」，提交後凍結草稿，缺少字型經 `prepareFonts` 統一下載（總進度+單一字型進度），完成後停在就緒頁，點「開始使用」才寫入偏好與初始資料（`commitFirstRunData` 語意，失敗留在精靈）並進入主視窗；失敗可重試、返回修改（作廢舊下載批次）或改用系統字型。關窗明確詢問繼續設定、使用預設設定或結束應用程式；未完成設定時選擇結束或直接結束應用程式均不建立筆記，下次啟動仍顯示精靈；歡迎文案隨分頁數產生（`welcomeTextFor`）；主視窗等精靈結束再建立。 |

| `internal/atomicfile` | 筆記與偏好設定共用的持久寫入：同目錄隨機暫存檔 → 完整寫入 → `Sync` → `Close` → rename → `SyncDir`；rename 後同步失敗用 `CommitError` / `IsCommitted` 表明目標已替換。Windows 不執行目錄控制代碼同步。`sweep.go`：暫存檔名前綴 `TempPrefix` 與按前綴+修改時間刪除崩潰殘留的 `RemoveStaleTemps`。 |
| `internal/sysnotify` | 未封裝執行時的回落通知投遞（無 MyGo 依賴，ADR-0005）：macOS 走帶 argv 的 osascript、Windows 走固定 AUMID 的 PowerShell WinRT toast（標題正文由環境變數傳入、XML 文字節點建構，命令隱藏且 10 秒逾時），點擊通知無回呼；投遞前統一淨化文字（`Sanitize`，封裝執行的原生通道也重用）：抹除 XML 1.0 不接受的 C0/DEL/C1 控制字元（否則 toast 的 XmlDocument 整條報錯），並按標題 100 / 正文 300 字元截斷（Windows 環境區塊總量 32767 字元，過長會讓 CreateProcess 失敗、通知靜默遺失）。 |
| `core/` | 共享資料核心（巢狀 module `zakki/core`，零第三方依賴的葉節點）：時間權杖（`@`/`~`/`^`）解析與正規寫回、重複規則與實例展開、當前實例及完成推進（ADR-0002）、原子時區選擇與 `RuleSyntaxVersion=1`；`core/zakkicore`（套件名 `zakkicore`）提供 UTF-16 位移及錯誤碼的 gomobile JSON 邊界；`core/model`（套件名 `model`）是 zakki.json 的共享磁碟模型單一事實來源（File/Tab/Settings、版本 3 移轉，含分頁數上限/預設值常數），桌面 `internal/notes` 以型別別名與轉發消費。桌面 `internal/markdown` 與未來行動端從這一份實作消費語意；分層鐵律見 [engineering-standard.md](engineering-standard.md) 第 3 節。 |
| `mobile/binding` / `mobile/ios` | 行動端腳手架（**已凍結**，2026-11-11：[ADR-0003](../adr/0003-pause-mobile-client.md)（中文）、[mobile-archive.md](mobile-archive.md)；不參與桌面 `make check` 的根 module 建置與測試）：`mobile/binding` 是獨立 Go module，`require zakki/core` 並 `replace => ../../core`，是唯一允許依賴 golang.org/x/mobile 的 module（tool directive 固定版本），供 `gomobile bind` 把 `core/zakkicore` 打包成 XCFramework／AAR；`mobile/ios` 是 XcodeGen 專案（`project.yml` 產生 .xcodeproj，產生物與綁定產物不入庫），不參與任何 Go 建置。core 仍是零依賴葉節點。 |
| `internal/markdown` | Markdown 解析、格式轉換、表格純函式（無 MyGo 依賴）。時間權杖（`@時刻`、`~提前量`、`^重複`）的行內辨識與原位更新（`timeinline.go`）與舊行尾解析（`rowtime.go`）已委託 `core`，本套件只保留呼叫面（含 `ParseRule` / `Expand` / `Occurrence` / `Advance` 薄轉發）並把行內程式碼/連結的 rune 保護區換算為 core 的位元組保護區；`AlertTiers` 與彈層共用一份檔位白名單。 |
| `internal/codehl` | 程式碼區塊逐行詞法著色純函式：`Kind`（註解/字串/數字/關鍵字/函式/鍵）× 行內 `Span`，按圍籬 info 正規化語言（go/js/ts/py/sh/json/yaml/toml/rust/c/java 家族，未知語言走通用規則）；不跨行追蹤，區塊註解/多行字串只著色起始行（無 MyGo 依賴）。 |
| `internal/notes` | 唯一權威檔案 zakki.json 的 `Store`（磁碟模型、移轉與未知欄位編解碼語意在 `core/model`，本套件以型別別名與轉發消費）：`store.go` 載入（經 `model.Decode`：傳回檔案存在性與原始 hadTabs，記住磁碟位元組指紋，並在鎖內儲存頂層/settings/tab 三層未知欄位快照；成功重讀作廢舊待寫快照與重試）、寫入保護（損毀/高版本/分頁超限/外部改動時拒寫，不產生副本）與路徑狀態；`save.go` 負責 `ScheduleSave`（首筆修改固定截止、後續輸入只換快照不延期）、`SaveNow`、結束 `Shutdown` 與失敗自動重試（寫入磁碟前組裝未知欄位快照後走 `model.Encode`：無未知欄位時與舊 `MarshalIndent` 輸出逐位元組相同，未知欄位跨儲存往返保留，已刪分頁的未知欄位不再落盤）；`relocate.go` 負責 `Relocate` 單一檔案安全遷址；`git_sync.go` / `git_schedule.go` 負責對這一個檔案的精確路徑 Git 提交與按間隔推送（背景單一 worker、代號門禁、本地寫入命令與遷址互斥、推送不佔鎖）；`sync.go` 負責雲端同步迴圈（先拉取遠端與版本號 → flush 待儲存 + verifyDisk → 判定表 → CAS 上傳/採用遠端/只更基線/自動合併，CAS 失敗重新拉取重判；本地階段持有 writeMu+gitGate 與遷址、寫入保護互斥，網路不佔鎖；基線存裝置本地狀態檔），`sync_merge.go` 是雙方都改時的逐分頁三方合併（ADR-0004：以基線原文為共同祖先，同一分頁兩邊都改較新者勝，不彈窗不留檔）、`FinalSync`（結束後限時補推）與儲存防抖/聚焦拉取/失焦推送的觸發語意；`sync_github.go` 是後端 A（GitHub Contents API blob sha CAS、大檔案 raw 回退；403/429 先看 `X-RateLimit-Reset` / `Retry-After` 給出等待提示；檔案 404 時用儲存庫探測區分「檔案缺失」與「儲存庫無權限/限流」，探測失敗原樣回報而不是當成遠端為空；回應超過 64MB 按失敗處理，不把截斷內容當正文）；`sync_oauth.go` 是 Device Flow 與權杖刷新（refresh token 輪換，`bad_refresh_token`/`invalid_grant` 映射為「重新連接」）；`sync_keyring*.go` 抽象系統安全儲存（macOS 鑰匙圈經 security CLI、Windows DPAPI 使用者級加密檔案、其他平台明確不可用；權杖供給閉包把「讀取→刷新→回存」串行化並跟隨呼叫方 ctx）。資料版本 3：排版主題舊 ID 隨載入移轉為 `note`。 |
| `internal/prefs` | `preferences.json` 模型、`Store`（修訂號/防抖寫入磁碟/異常門禁）。偏好版本 15：新增 `sync.clientId`（GitHub App Client ID，非憑證，空字串回落環境變數 `ZAKKI_GITHUB_CLIENT_ID`）。版本 14：新增 `sync` 節（雲端同步開關 `enabled`、後端 `backend`、儲存庫 `repo`；全部為非憑證設定，token 只進系統安全儲存，絕不寫入偏好檔案；開啟時驗證後端與 owner/name 儲存庫）。版本 13：新增 `appearance.padX` / `padY` 正文內距覆蓋；版本 12 新增 `storage.dataFile`（筆記檔案絕對路徑，空字串 = 應用程式資料目錄 `zakki.json`）與 `storage.saveIntervalMs`（預設 2000），新增 `git.autoCommit` / `git.autoPush`（預設關）與提交/推送間隔（預設 1 / 10 分鐘），並移除 `backup` 整節與 `advanced.autosaveDelayMs`。版本 11：`advanced.windowColumns` / `windowRows` 取代像素寬高（`WindowPixels` / `WindowGrid` 換算）。版本 10：`appearance.style`（標準 / 手帳），生效值經 `EffectiveColorTheme` / `EffectiveMarkdownTheme`。更早版本差異見 configuration.md。 |
| `locales/` | MyGo i18n 目錄：`en.json`（來源）+ `ja.json` / `zh-Hans.json` / `zh-Hant.json`，ICU MessageFormat；`go tool mygo generate` 產生 `catalog_gen.go`。`internal/app` 統一經 `locales.*` 取文案；語言選擇在首次啟動精靈第一步與設定一般頁，變更經 `App.SetLocale` 生效並由 `OnLocaleChanged` 重建選單。 |
| `internal/app/datamigrate.go` | 資料目錄解析：正式安裝 `<系統應用程式資料>/Zakki`，開發執行 `Zakki Dev`（`resolveDataDir` 內 `sync.Once` 快取，解析失敗不建構 Store）。應用程式不掃描或移轉舊目錄；目前目錄或已設定路徑的資料照常讀取，找不到目前資料時才按全新安裝處理。 |
| `internal/app/live_markdown.go` / `internal/app/view_highlight.go` | 橋接 `internal/markdown`；原始碼高亮 `editorRanges`（依賴 `ui.TextRange`），用每 rune 一位的佔用位圖判交，保留搜尋優先與重疊候選整段拒絕；程式碼內容行按外觀「程式碼配色」方案套 `internal/codehl` 區間。行內時間權杖在原始碼模式按 `@` 強調色、`~` 次級色分色，元件後的正文不染色。 |
| `internal/app/view_codehl.go` | 程式碼配色方案表 `codeSchemes`（柔和/明快 × 淺深）與解析 `codePaletteFor`；`codeLineSpans`（即時預覽 RichText）與 `codeSourceRanges`（原始碼 TextRange）把 `internal/codehl` 詞法片段套色。 |
| `internal/app/menu_commands.go` | 指令註冊表 `commandRegistry`（ID/名稱/分組/預設綁定，Markdown 指令 + 全域喚出）：選單、Keymap 設定頁、視窗兜底與編輯器共用；加速器解析/正規化/顯示名（`parseAccelerator`/`accelerator`/`displayName`）、固定綁定表（`fixedBindingsForPlatform`）。 |
| `internal/app/keymap_bindings.go` | 改鍵驗證與寫入：平台預設鍵（`defaultBindingForPlatform`：標題退到次級修飾鍵 mac ⌃N / 其他平台 Alt+N）、錄鍵驗證（`validateBinding`）、衝突偵測（`findBindingConflict`，含固定鍵與系統保留鍵）、單項寫入/回復與全部回復。 |
| `internal/app/menu_edit.go` | 編輯指令路由：正文聚焦時，選單列與右鍵選單的復原、重做及剪貼簿操作共用文件事務；搜尋、分頁重新命名和設定中的輸入保留系統編輯角色。焦點切換時 `refreshEditMenuRouting` 只刷新編輯子選單項目（系統 Role ↔ 應用程式 Click），不全量 `buildMenu`。右鍵指令在主視圖影格末執行，避免控制項建構期間重複記錄歷史。`copyTabTextToClipboard` 供選單與快速鍵兜底共用。 |
| `internal/app/live_format.go` | 格式指令執行（`execFormatCommand`）：標題/清單/引用/行內/程式碼區塊/插入在即時預覽與原始碼模式共用；無選取區切換「待輸入格式」意圖（`mdPending`，輸入/貼上/IME 確認時經 `pendingWrapFor` 產生有效 Markdown）；指令以獨立復原事務執行（`editCommand`）。 |
| `internal/app/live_link.go` | 連結浮層 `linkTool`：開啟、驗證並寫回連結。 |
| `internal/app/settings_window.go` / `internal/app/settings_ui.go` / `internal/app/settings_storage.go` / `internal/app/settings_keymap.go` | 原生設定視窗：單一實例非模態（`StateKey: "settings"`），五頁導覽（一般、分頁、AI 助理、儲存與同步、快速鍵）與草稿綁定；變更經 `updatePreferences`；AI 頁在 `settings_ai.go`（頂端是啟用開關，下方是服務連線與傳送範圍；測試連線使用目前表單，進入 AI 頁時從已儲存的位址自動取得模型列表，變更提供者、位址或金鑰會使舊列表失效）；「儲存與同步」頁在 settings_storage.go（檔案路徑、儲存狀態與 Git 開關，見 storage_file.go 行）；快速鍵頁點擊徽標錄鍵、衝突就地「改用至此」（錄鍵期間經 `setKeymapRecording` 臨時摘除選單與全域快速鍵，結束時重建；驗證與寫入在 keymap_bindings.go），全域快速鍵先註冊新鍵再註銷舊鍵。欄位契約見 [configuration.md](configuration.md)。 |
| `internal/app/sync_bridge.go` / `internal/app/settings_sync.go` | 桌面雲端同步橋接：`configureSync` 把偏好 sync 節裝配為 `notes.Store` 的同步設定（後端 A + token provider + 基線狀態檔路徑，隨每次偏好套用重放，但僅在儲存庫/Client ID/端點/安全儲存變化時重建後端實例——Store 按指標身分判設定變更，新指標會作廢在途輪次並丟掉防抖排程）；同步觸發（啟動、視窗啟用、儲存後防抖、手動）與狀態泵 `syncPump`（遠端已套用或合併完成則重載筆記並重排提醒並提示；視窗聚焦拉取（限頻 30s）、失焦/隱藏時未推送修改立即推送（限頻 5s）、結束時經 `FinalSync` 限時 3s 補推，失敗不阻止結束）；連接流程 `connectGitHubSync`（Device Flow：裝置碼 → 使用者碼彈層在獨立 goroutine 展示不阻塞輪詢 → 權杖存系統安全儲存；Client ID 取偏好 `sync.clientId`，空時回落環境變數 `ZAKKI_GITHUB_CLIENT_ID`，`ZAKKI_GITHUB_BASE_URL` 僅供自測注入本地假服務）、中斷連接清憑證；`open_url.go` 只接受 https 的授權頁位址並在預設瀏覽器開啟（`open` / `rundll32`）；憑證存在與否只在開啟設定頁、連接/中斷後讀取並快取，不在建構影格裡做系統呼叫；「儲存與同步」頁雲端同步分區（settings_sync.go：開關、儲存庫、Client ID、連接/中斷、立即同步、開啟授權頁、狀態行與 Git 暫停說明橫幅，缺 Client ID 時顯示原因；雙方都改時自動合併，無衝突入口）。 |
| `internal/app/settings_general.go` | 「一般」頁骨架：一般（語言、視窗）在前，隨後由 `settings_ui.go` 的 `appearanceSections` 建構主題、排版、程式碼、頂端列分區，整頁共用受視窗高度約束的捲動區與單一重設入口；重設同時回復視窗與外觀，保留儲存、Git、快速鍵和筆記。預設視窗大小按每行字數 × 行數輸入，附目前字級換算的像素。偏好異常的復原入口保留在頁頂。 |
| `internal/app/settings_select.go` | 設定頁下拉：`boundedSelect` 與 `ui.Select` 同外觀與鍵盤行為，彈層限高 300 並在內部捲動（`ui.Select` 不限高，選項多時被視窗裁掉）；`selectTrigger` / `selectPanel` 供字型選擇器共用。 |
| `internal/app/view_theme.go` | 雙層主題：`colorThemes`、`markdownLayouts`；經典顏色主題在主視窗與其他介面一樣走中性調色板（`classicNeutralPalette`），`dotThemes` 只提供分頁圓點與焦點 accent；狀態列僅小圓點保留分頁色，正文按對比度選中性色、持久錯誤用主題危險色；`resolveStyle` 合併顏色 + 排版 + 使用者覆蓋，結果按（外觀偏好、圓點、明暗、是否覆蓋、本機字型清單身分）快取最近 4 份（`fontRuntimeState.styles`），字型堆疊拼接與本機字型查找不再每影格重複；排版可帶裝飾尺寸（引用條寬、程式碼圓角/描邊，手帳主題使用）。字型堆疊在 Apple 字型之後列 Windows 回退（微軟正黑體、新細明體、Segoe UI、Consolas），macOS 繪製不變。 |
| `internal/app/view_sketch.go` | 手帳排版的手繪裝飾：種子確定的抖動鉛筆線（`sketchLine`/`sketchRect`）、波浪分隔線、螢光筆標題底（一/二級）與鉛筆底線（三級及以下）；標題裝飾容器 `sketchHeadingFrame` 在預覽態與編輯態共用。點陣紙與頁邊紅線、馬克筆引用條、手繪勾選框（`sketchCheckbox`，保留核取方塊語意）與清單小圈；表格格線與程式碼框為鉛筆線；手繪顏色主題內容區不透明（`paperBackground`）。 |
| `internal/app/window.go` | 視窗顯示與隱藏：`showWindow`、`toggleWindow`、`applyAlwaysOnTop`；`minimumWindowSize` 按目前字級、行高與內距保證至少 10 欄、4 行正文並預留搜尋列空間，建立視窗和偏好套用時同步原生最小尺寸；`defaultWindowSize` 按目前生效排版（`windowMetrics`：正文字級 / 行高 / 內距覆蓋或排版預設、平台頂端列 + 狀態列）把偏好裡的每行字數 × 行數換算成新建主視窗的像素；建立主視窗時觸發本機字型列舉（`loadFontFamilies`）；`resetWindowUI` 在建立新原生視窗前清理舊 `ListState` 和剪貼簿服務，並還原編輯器選取區同步，文件與復原歷史保留。 |
| `internal/app/view_status.go` | 狀態列文案與字數統計：`statusWarn`（僅持久異常：儲存/載入被暫停，一次性提示已改走 toast）、`countWords`、`saveLabel`；`docStats` 按文件串快取，同一分頁 32KB 以上的文件連續輸入時每 250ms 至多重算一次，節流期沿用舊值並用 `time.AfterFunc` + `Window.Update` 預約一影格補算；切換分頁立即重算。 |
| `internal/app/view_toast.go` | 居中 toast 提示：一次性操作回饋（分頁上限/保底、匯出成功、字型快取異常、重讀失敗、結束儲存失敗）經 `showToast` 排隊，主視窗每影格由 `toastViewport` 消費並交給 MyGo toast 引擎（`ToastViewportBase`/`ToastBase`）在視窗正中展示深色氣泡，約 4 秒自動消失，懸停/失焦暫停計時，相同文案替換重新計時；持久異常不走 toast，仍由狀態列常駐展示。 |
| `internal/app/app_state.go` | `app` 由領域子結構體匿名嵌入，另加 `cfg`、`ready`、`now` 與 `notes.Store` / `prefs.Store`。 |
| `internal/app/state.go` | 筆記載入編排與搜尋比對；寫入磁碟委託 `notes.Store`。`seedInitialContent` 統一產生歡迎文字與「時間與提醒」使用說明：重用第二個初始分頁，只初始化一個分頁時合併到歡迎文字；不向既有分頁補寫。 |
| `internal/app/tab_model.go` | 動態分頁：`tab`、`DefaultTabCount`（7）、`MaxTabCount`（9，數字快速鍵全覆蓋），以及目前分頁的存取與增刪改。`switchTab`/`addTab` 切換後聚焦末區塊輸入區、游標在文件末尾（即時預覽模式；原始碼模式沿用分頁工作階段還原選取區）。 |
| `internal/app/tab_export.go` | 「匯出為 Markdown…」（檔案選單與分頁右鍵選單）：主執行緒取快照（即時預覽先 `commitDraft`），背景彈原生儲存對話框，建議檔名取分頁名並替換檔案系統保留字元；經 `atomicfile` 原樣寫出，失敗彈原生錯誤框，成功以居中 toast 提示。 |
| `internal/app/tab_strip.go` | 分頁列液態動畫與重新命名幾何；標記只有圓點與圓環；目前標記的高亮度調色板色（黃、淺粉等）在淺色主題下向黑色收斂最多 18% 提高對比，深色與深色主題原樣。未命名時全部同大；有名字時名字畫在目前分頁的色條裡，背景與文字共用 `tab_palette.go` 的衍生配色。有標題的補間只在標題完全淡出期間切換底色，字色不插值；連續切換不復活已淡出的標題，進入重新命名時直接在目標位置就位。圓環描邊且不參與液態橋接。`TabMotionOff` 關閉補間（與系統 ReduceMotion 同路徑）。頂端列拖曳排序：圓點按鈕 `Drag(tabDrag)`，分頁條為落點，經過時畫插入指示線，放開記入 `dropPending`、影格末經 `reorderTab` 重排（按下不動仍是點擊，雙擊重新命名與右鍵選單不受影響）。分頁右鍵選單（重新命名/前移/後移/匯出/刪除，與設定頁、選單列和固定快速鍵共用既有操作）：選單動作記為待執行、主視圖影格末按穩定 ID 重新定位後執行（`runTabMenuPending`），不在建構期改分頁集合；重新命名先切到被右鍵的分頁，與雙擊同路徑。 |
| `internal/app/tab_palette.go` | 膠囊顯示配色：優先柔白 / 墨黑，按 WCAG 相對亮度爭取 5:1；背景最多向黑白混合 12%，不足時讓文字向黑白極值收斂，穩定態不低於 4.5:1。僅衍生顯示色，不修改分頁原色與持久快照；標題繪製和重新命名輸入共用配色。 |
| `internal/app/settings_tabs.go` | 設定視窗「分頁」頁：分頁清單是可拖曳排序的 List（`ListState.Reorder` → `reorderTab`，active 按穩定 ID 跟隨）：行首為與頂端列同形的小圓點（點開 `ui.Popover` + `ui.ColorPicker` 取色，含常用色板；草稿以 `tabWellSeeded` 基線防 `Changed` 遲到誤回播種）、雙擊行內重新命名（工作階段狀態獨立於頂端列膠囊重新命名）與前移/後移、刪除按鈕；各按鈕保留鍵盤焦點環。頂端列外觀項（形狀/標題寬/動畫速度/動效/玻璃強度）在「一般」頁的頂端列分區，見 settings_ui.go 的 `tabAppearanceSection`。 |
| `internal/app/view.go` | 主視窗骨架：`view` 每影格建構頂端列、搜尋列、原始碼編輯區與底列；頂端列左側「關閉視窗」呼叫 `Window.Hide`，保留筆記、分頁和原生繪製資源，避免每次喚出都重建 Metal 管線；Dock、系統匣或全域快速鍵可重新顯示。選單關閉（⌘W）仍銷毀視窗，後續由 `showWindow` 重建；結束流程不被隱藏邏輯攔截。頂端列按 `c.TitleBar()` 避讓系統視窗按鈕：右側內距加按鈕帶寬，Windows 上頂端列降到按鈕帶同高（32），內容與按鈕 glyph 同軸垂直居中，無按鈕帶時保持 38。只有一個未命名分頁且不在重新命名時隱藏分頁區（`tabStripHidden`）；單一命名分頁仍顯示標題與右鍵選單入口；並復位分頁幾何避免還原多分頁時殘留補間。頂端列、底列與搜尋列的圖示按鈕統一走 `chromeIcon`（16pt 圖形、24pt 點擊區、懸停淺底、名稱兼作無障礙標籤與懸停提示）；頂端列圓點懸停顯示分頁名與切換快速鍵（`tabTooltip`）。底列文字單行截斷並帶懸停全文，窄視窗下切換圖示不被擠出。 |
| `internal/app/find_bar.go` | 搜尋列與命中跳轉：輸入框 + 計數 + 跨分頁開關 + 上/下/關閉圖示；`openFind(allTabs)` 由 ⌘F / ⇧⌘F 共用。跨分頁模式下計數按分頁快取（`findState.tabHits`，鍵為文件內容 + 查詢），上/下一個越過目前分頁末尾時切到下一個有命中的分頁。Enter 下一個、Shift+Enter 上一個（`findStep`，選單搜尋下一個/上一個經 `findOpenOrStep` 共用）；`highlightQuery` 只在搜尋列開啟時傳回查詢，原始碼區與即時預覽（標題、段落、任務項，以及含命中的單行引用/清單）共用它高亮命中，關閉搜尋列即清除高亮。 |
| `internal/app/view_live.go` | 即時預覽區塊清單：`livePane`（`ui.List` 虛擬化，只建構可見區塊；外層 Column 兜底左右留白與行間空隙的空白點擊）、焦點區塊 `activeText` 與非焦點區塊繪製；清單顯式綁定解析後的正文顏色、字型與行高，避免影格末還原外框主題時覆蓋內容繼承。 |
| `internal/app/live_blocks.go` | 按行切塊：`liveBlocks` / `parseLines`。焦點區塊保留排版並編輯可見文字；Markdown 標記留在原始文字中。 |
| `internal/app/live_edit.go` | 草稿提交與焦點：`commitDraft`、`publishDraft`、`focusBlock`、區塊快取與 rune 首位移表。文件重切換代時清理預覽衍生快取；位移表重建配置新切片，不覆寫目前建構影格持有的舊代。 |
| `internal/app/live_preview_cache.go` | 目前文件區塊世代的有限快取（最多 64 區塊）：可見文字投影、表格 cell/位移與程式碼 token spans；只儲存純資料，不儲存 UI 元素/Context，不保留歷史版本。切分頁、重切和編輯失效，程式碼顏色隨完整 palette 失效；仍由原控制項整體版面配置單一大型區塊。 |
| `internal/app/live_click.go` | 預覽點擊落點定位：文字元素自己接收點擊（`bindPreviewClick`），`liveHit` 記錄區塊號、`ui.Handle` 與文字投影，元素只活一影格，點擊回呼裡再 Resolve；文字鋪滿行寬，點在第 n 行右側空白時游標停到該行末尾而非整區塊末尾，行間距/頁邊用同一元素上一影格盒子換算。 |
| `internal/app/live_input.go` | 邊界鍵：Enter 分段與清單續行、退格合塊、方向鍵跨區塊；程式碼區塊內 Enter 委託 `applyCodeEnter`。 |
| `internal/app/live_codeblock.go` | 圍籬程式碼區塊專屬編輯：Tab/Shift+Tab 縮排（`applyCodeTab`，單位取一般頁程式碼分區的 `appearance.codeIndent`，退化圍籬落到合成內容行）、Enter 續行縮排（`applyCodeEnter`，繼承行首縮排、開括號再加一級）、右下角語言徽標（`codeLangBadge`：FocusBind 聚焦、失焦/Enter 提交、Esc 取消，改寫圍籬 info 串不經 `applyDocEditAt` 以免搶主編輯器焦點；工作階段態 `codeLang*` 存 `liveEditorState`）。 |
| `internal/app/live_virtual.go` | 區塊內虛擬化：200 行以上的程式碼區塊預覽與表格（含正在編輯的表格）只建構視口上下各一螢幕的行，其餘用佔位撐開；位置取上一影格 `Bounds` 並按捲動量校正，行高按建構過的行實測快取（`liveVirtState`，鍵為區塊原始碼 + 區塊類型 + 參與版面的字型、字級、行高、表格 padding 與排版變體，影格末清掉未用項目），未測行用實測均值；正在編輯的表格行總會建構；視窗依據過期（首影格或剛捲動）時再要一影格校正。單一超長段落仍整段排版。 |
| `internal/app/notify.go` | 系統通知投遞與點擊回落（ADR-0005）：在主執行緒的 `App.WhenReady` 判定通道並快取；就緒前的通知僅當次使用備援通道，不快取判定。封裝執行（`mygo dev` / `mygo build` 產物）用 MyGo 原生通知（macOS UNUserNotificationCenter、Windows 系統匣氣泡），拿到真實應用程式圖示與點擊回呼；未封裝執行（裸 `go run` / `go test`）退回 `internal/sysnotify`。兩通道共用 `sysnotify.Sanitize`。使用者拒絕授權映射為 `errNotifyDenied`，只在應用程式內提示一次且不回落（不繞過系統設定繼續彈橫幅）。通知 ID 為 `zakki-reminder/<分頁持久ID>/<實例dueMs>/<行號>`，同一實例重發替換舊通知；`App.OnNotificationClick` 在 `App.Run` 之前註冊（macOS 由點擊啟動應用程式時回呼早於 `WhenReady`），點擊後 `showWindow` → 按持久分頁 ID 切分頁 → 行號命中則聚焦該區塊，行號漂移按 due 兜底，分頁已刪除只喚起視窗；原始碼模式沿用工作階段選取區不搶游標。 |
| `internal/app/live_task.go` | 一般與手帳核取方塊共用 `toggleTask`：未勾選的合法重複任務經 `Advance` 推進錨點，保留提醒和規則且保持未勾選；序列結束才勾選，取消勾選沿用一般切換；一次文件事務支援復原/重做。 |
| `internal/app/live_widgets.go` | 焦點區塊輸入（`applyProjectedHint`）與表格儲存格編輯控制項。 |
| `internal/app/live_timechip.go` / `live_timeprojection.go` / `live_timeview.go` | 時間元件的文案、狀態與行內物件呈現：預覽和聚焦保持同一元件位置與樣式，`liveProjection` 中單一 U+FFFC 對應完整時間原始碼。原生物件尺寸參與換行與游標版面；前後均可輸入，元件後正文保留原位。合法重複任務的 chip 經 `Occurrence` 顯示當前實例，當天即使過點仍顯示今天，過去或結束實例保持中性、不標逾期。首次點擊進入條目時游標預設在條目末尾；搜尋選取區觸及權杖時擴展到整個元件。原始碼模式仍可直接修改權杖。 |
| `internal/app/live_timepopover.go` / `live_timerecur.go` | 時間彈層：DateInput 與 TimeInput 共享草稿時間，日期變更也啟用完成；重複選擇器經核心 `ParseRule` 播種預設或自訂（間隔、單位、每週週幾及可選結束日期），預設切換同步自訂欄位，未改規則保留 `^` 原文，選擇「無」清除重複；軟換行區塊只更新權杖所在原始碼行；點擊時刻浮出可捲動的小時／分鐘選擇面板（12 小時制另有上下午欄），輸入控制項仍可鍵盤操作；提醒檔位 pill（`markdown.AlertTiers` 白名單，自動換行）、清除與完成；寫回走勾選切換同一條管線並進復原。`⌘⇧0`（list.taskTime）或點擊 chip 開啟；行尾鍵入 `@` 即時喚起（佔位符就地轉化，對齊表格即時轉換心智，程式碼區塊與行中不觸發）；原始碼模式無錨點不回應。 |
| `internal/app/reminders.go` | 提醒排程：主執行緒只截取全部分頁的 ID/標題/文字快照，250ms 防抖背景按分頁文字快取解析區塊；編輯立即作廢舊代。首次啟動僅排未來觸發；執行期新提醒提前量超過剩餘時間時立即投遞，已排未投遞項遲到不超過截止後 60 秒仍投遞；30 秒牆鐘巡檢並重排，以分頁 ID、實例截止、提前量、正文 hash 去重；編輯前已排未投遞資格按分頁 ID、實例截止、提前量繼承，保證正文變化跨截止仍用最新正文投遞。已投遞記錄保留 24 小時。刪除、改名、重讀成功後重排；結束失敗還原計時器與巡檢，成功結束停止投遞。合法 `^` 經 `markdown.Expand` 展開未來 60 天、最多 64 個實例，每實例獨立計算截止減提前量；解析區塊按文字快取，實例每次按牆鐘重展，30 秒巡檢直接消費最近的不可變分頁快照，主視窗銷毀後仍續排；另保留一分鐘遲到視窗交由既有排定資格門禁判斷。投遞 ID 與點擊語意見 `internal/app/notify.go`。狀態受 `reminderScheduler` mutex 保護。 |
| `internal/app/live_inline_style.go` | 行內樣式唯一映射 `inlineStyle`：粗體、斜體、程式碼字型／底色、連結顏色／底線；投影預覽、清單／引用 RichText 子元素與原生編輯區間共用定義。 |
| `internal/app/live_projection.go` | 可見文字投影與 rune 映射（`projectBlock`/`replaceSelection`）；`textRanges` 把共用行內樣式轉換為原生編輯區間，搜尋命中（`searchHit`：強調色 + 粗體 + 強調色淡底）覆蓋顏色、字重與底色，保留字型、斜體與裝飾；文件級復原事務（`liveEdit` + `editKind`：連續輸入合併、貼上/IME/格式指令獨立），`undoLive` 覆蓋即時預覽與原始碼模式，還原文字與選取區。 |
| `internal/app/live_selection.go` | 文件級全選（`selectDocWide`/`menuSelectAll`）：跨區塊選取區高亮與複製/剪下/刪除/輸入在 app 層處理；表格儲存格內保留儲存格原生全選。 |
| `internal/app/live_history.go` | 大文件復原差分：64KiB 起將非堆疊頂的小改動存為複製的位元組差分；整篇替換仍共享原快照，避免反向放大記憶體。復原/重做各自堆疊頂保留完整快照做精確文字驗證，用已驗證的相鄰事務展開下一個堆疊頂；不靠整文件 hash、不減少 200 條歷史、不擴大輸入合併範圍。差分發布後不可變；彈堆疊清零底層槽以釋放全文參照。復原歷史只屬於目前分頁的這段編輯：切換到別的分頁（含新增分頁、刪除目前分頁）即清空，不按分頁保留，也不寫入磁碟；刪除其他分頁或排序不影響目前歷史。原始碼模式的分頁工作階段（`sourceSession`）只記游標與選取區。 |
| `internal/app/table_edit.go` | 表格編輯：`createTableOnEnter`、`navigateTable`、`tableEnter`。 |
| `internal/app/table_actions.go` | 表格工具狀態 `tableTools` 與行列增刪。 |
| `internal/app/table_toolbar.go` | 表格懸浮工具列。 |
| `internal/app/live_syntax.go` | 區塊級類型與 `parseBlocks`（測試/預覽適配）；行內解析與格式轉換見 `internal/markdown`。 |
| `internal/app/view_icons.go` | 內嵌 Lucide 圖示（ISC 授權），24×24 viewBox、2px 圓頭描邊，`ui.MustParseSVG` 產生。 |
| `internal/app/tray_icon.go` / `internal/app/assets/tray.png` / `internal/app/assets/tray-black.png` | 系統匣圖示：內嵌「墨 Z」手寫字母 PNG（`trayPNG`，36×36px，與應用程式圖示配套，白墨/黑墨兩版）；macOS 以模板自動適配選單列明暗，其他平台系統不管模板，按桌面明暗選墨色並隨 `Theme.OnUpdated` 切換，避免圖示隱入同色工作列。`assets/tray.svg` 保留向量來源。 |
| `internal/app/shortcut_fallback.go` | 視窗級快速鍵兜底：每影格從統一鍵位表（指令綁定 + 固定鍵 + 使用者覆蓋）註冊 `Context.Shortcut`，動作與選單 Click 同一實作。macOS 選單 accelerator 在系統層先消費、兜底不觸發；Windows 原生 surface 不觸發選單 accelerator，兜底即實際觸發路徑。編輯角色六鍵由 ui 引擎內建處理不重複註冊。 |
| `internal/app/shortcut_text.go` | 靜態快速鍵文案的平台轉換：locale 字串以 macOS 符號書寫，非 macOS 展示前經 `shortcutText` 轉成 Ctrl/Alt/Shift（⌃ 即 mac 標題鍵映射為 Alt）；動態鍵位展示走 `keyBinding.displayNameForPlatform`。 |
| `internal/app/font_runtime.go` / `internal/app/font_picker.go` | 字型執行期與選擇控制項：背景 `ui.FontFamilies()` 列舉本機字型並與遠端目錄去重（本機字型以 `local:<家族名>` 鍵入選項，經 `fonts.ClassifyFamily` 按家族名歸入中文 / 英文字型，程式碼字型只列等寬家族）；`fontPicker` 是設定頁與精靈共用的字型下拉（搜尋、推薦 / 本機字型分組、每項樣字、限高捲動），鍵直接綁定，預設字型別名只在顯示時對齊（`canonicalFontKey`，空字串不觸發下載，不能寫回）；`prepareFonts` 是唯一準備入口——缺少的可下載字型按固定目錄逐個 `fonts.Store.Ensure`（SHA256/位元組驗證、可取消、進度經存活視窗 `Window.Update` 回主執行緒）後 `ui.RegisterFont` 私有註冊，完成後只回呼 `settle(ok)`。字型應用程式私有註冊，不寫入作業系統字型庫；本機字型解除安裝後保留偏好，繪製時回退並提示（`usableFontKey`）。舊使用者升級後首次開窗由 `preparePreferredFontInBackground` 靜默補齊既有偏好引用的缺少字型，失敗只記紀錄。精靈與設定頁共用選項與準備流程；失敗重試、返回修改或換選會作廢舊批次（generation 門禁）。 |
| `internal/fonts` | 純邏輯字型資源模組（無 MyGo 依賴）：固定遠端目錄（LXGW WenKai Lite / JetBrains Mono / Fira Code 5.2，均為官方 GitHub 固定 commit 的原始 TTF，OFL-1.1，見 `catalog.go`）；`Store.Ensure` 先驗證快取（`<應用程式資料>/fonts/<資源ID>.ttf`，長度+SHA256），命中不觸網，缺失才下載：HTTP 200、嚴格等長、暫存檔同目錄隨機名 0600、Sync+Rename 原子發布，失敗/取消不留半截檔案；`LocalKey`/`LocalFamily` 驗證 `local:` 鍵防字型堆疊注入；`ClassifyFamily`（script.go）按家族名推斷中文 / 西文 / 其他文字 / 符號，供字型下拉歸類。Dank Mono 等商業字型不在目錄，僅本機已裝時可選。 |
| `internal/app/testdata/LXGWWenKaiLite-Regular.ttf` | 手寫字型堆疊的無頭測試夾具（原 `resources/` 內建字型遷入），僅 `go test` 讀取，不隨包散布；`font_hand_test.go` 用它驗證註冊路徑與字型堆疊安全回退。 |
| `resources/icon.png` / `resources/icon.svg` | 「墨 Z」應用程式圖示：米色紙上一筆手寫 Z 加朱紅句點（1024×1024 PNG 與 SVG 原始檔），MyGo 建置時產生 macOS ICNS 與 Windows ICO。 |

## 執行緒模型（MyGo 硬約束，違反會在執行期出錯或被 `go tool mygo vet` 攔下）

- **app 狀態只在主執行緒改**。`view` 每影格在主執行緒讀狀態建構 UI；選單/系統匣/快速鍵回呼經
  `withWindow` 回主執行緒改狀態並觸發重繪；背景完成回呼只經原子發布的活視窗 `Window.Update` 投遞，視窗關閉後直接丟棄。
- **背景寫入磁碟與提醒解析只接觸不可變快照**。提醒主執行緒複製分頁 ID/標題/文字，背景按文字快取解析區塊並計算投影；筆記經 `snapshotFile`、偏好經 `prefs.Store.TakeSnapshot`，
  均在主執行緒拍值複製；背景計時器只寫快照，絕不讀寫 `app` 的可變欄位；
  協調狀態在 `notes.Store` / `prefs.Store` 內部 mutex 中，偏好另用修訂號防舊覆蓋新。
- **每影格建構的 UI 元素（`ui.*` 控制項）不存入 `app`**。`app` 裡只保留視窗/選單這類桌面整合控制代碼
  （`*mygo.Window`、`*mygo.Menu`、`*mygo.MenuItem`），一般跨影格狀態一律存一般欄位。
- **跨執行緒刷新用 `Window.Update`**。`runOnMain` 只讀取原子發布的活視窗並投遞；視窗不存在時不執行回呼，絕不退回背景 goroutine 直接改 `app` 狀態。
- 結束流程（`finishQuit` → `shutdownForQuit`，主執行緒）：停儲存計時器 → 置 `stopped` → 最終快照儲存並等它完成。
  筆記與偏好設定均成功後才結束（更新器的重新啟動也經過此流程）；任一儲存失敗則取消結束、還原筆記 Store 接收儲存，
  重開主視窗顯示錯誤，還原提醒排程與牆鐘巡檢，允許繼續編輯與重試。

## 持久化生命週期

應用程式資料目錄為 macOS 的 `~/Library/Application Support/Zakki/` 或 Windows 的 `%APPDATA%\Zakki\`；開發執行使用同層的 `Zakki Dev`。

| 內容 | 備份與同步 |
|---|---|
| `zakki.json` | 唯一的筆記檔案，含分頁內容和部分分頁列設定。備份實際使用的檔案：預設在應用程式資料目錄，也可由 `storage.dataFile` 指向其他位置。應用程式的 Git 自動提交和雲端同步只處理這個筆記檔案。 |
| `preferences.json` | 應用程式偏好；需要保留設定時另外備份。還原到其他裝置時，確認 `storage.dataFile` 等本機路徑。 |
| `window-state.json`、`updater.json`、`fonts/` | MyGo 視窗狀態、更新器狀態和可重新下載的字型快取。不需隨筆記備份或同步。 |
| `sync-state.json` | 裝置本機的同步基線，含上次同步的筆記原文。不是獨立備份，請勿提交至 Git 或跨裝置複製。缺少此檔時視為首次同步：有雲端筆記就採用雲端，否則上傳現有本機筆記。 |
| AI 金鑰與 GitHub 同步憑證 | macOS 存在系統鑰匙圈；Windows 以目前使用者的 DPAPI 加密後，存放在應用程式資料目錄中的 `ai-key.bin`、`sync-token.bin`。筆記與偏好不含憑證，請勿提交或同步；更換裝置後請重新儲存金鑰或授權。 |

目前程式碼不讀寫版本 12 之前的 `backups/`；清除前請先確認沒有需要保留的舊筆記。

不落盤的東西：

- **紀錄**：`setupLogging` 只寫 stderr，不建立紀錄檔；也不呼叫 `mygo.App.Path(mygo.PathLogs)`（它會順手
  建立 `~/Library/Logs/Zakki/`）。持久化錯誤的如實回報靠介面（底列警示、設定頁狀態），不靠紀錄。
- **復原歷史**：`liveUndo` / `liveRedo` 只在記憶體，切分頁清空，結束即消失。
- **損毀偏好的副本**：回復預設時直接覆蓋損毀的 `preferences.json`，不另存原檔（見「偏好生命週期」）。
- **崩潰殘留的暫存檔**：原子寫（`.<檔名>.tmp-*`）、遷址（`.zakki-relocate-*`）與可寫探針
  （`.zakki-write-probe-*`）正常路徑立即刪除；行程在寫入瞬間被殺時，下次啟動由 `sweepStaleTemps` 在資料目錄、
  筆記檔案目錄裡刪除超過 10 分鐘的殘骸（閾值避開共享資料夾裡另一行程正在寫的檔案）。
  前綴規則在 `atomicfile.TempPrefix`，清掃在 `atomicfile.RemoveStaleTemps`。

### 儲存（唯一權威檔案：`storage.dataFile` 指向的 `zakki.json`）

1. **固定截止**：首筆未儲存修改起 `storage.saveIntervalMs`（預設 2 秒）後開始寫入磁碟；
   期間繼續輸入只替換待寫快照，不延後截止時間；無修改不重複寫。失敗自動按同一間隔重試。
2. **持久寫入**：`notes.Store.writeFile` 先寫同目錄隨機暫存檔，完整寫入並 `Sync` / `Close` 後 rename，再同步父目錄（Windows 不執行目錄同步）；替換已完成但目錄同步失敗時仍更新磁碟指紋並保留儲存錯誤，重試會重新寫入和同步，成功前不報已儲存。寫入磁碟前 `verifyDisk`
   核對磁碟位元組指紋，檔案被外部改動/移動/刪除（`ErrExternalChange`）或處於寫入保護
   （`ErrSaveBlocked`）時拒絕寫入並回報，絕不用記憶體內容盲寫覆蓋。
3. **最終儲存**：結束（含更新重新啟動）經 `Shutdown` 等待在途寫入並寫最終快照；失敗阻止結束、
   保留編輯工作階段，允許修復後重試。停止後新到的 `SaveNow` 傳回 `ErrStopped`；選單儲存將結束期間的遲到請求視為無操作，一般同步呼叫仍能獲知未寫入。
4. **遷址**：更換儲存位置走 `Relocate`——拒絕覆蓋既有目標 → 寫入並驗證候選檔案 → 偏好設定持久化
   成功後才切換權威路徑 → 刪除舊檔案；提交前失敗保留原路徑與原檔。`RelocateResult.Committed` 明確區分切換是否完成，提交後的舊檔案清理或偏好目錄同步失敗仍採用新路徑並回報警告；候選檔案優先以硬連結排他發布，不支援硬連結時以 `O_CREATE|O_EXCL` 排他寫入、同步並回讀驗證；目錄先同步，再允許偏好指向它。

### Git（檔案位於儲存庫內且使用者開啟後，直接追蹤該檔案）

`git_sync.go` / `git_schedule.go`：背景單一 worker，提交與推送各自按間隔排程（預設 1 / 10 分鐘，
失敗也計一次嘗試）；提交只 `add`/`commit` 目前筆記檔案的精確路徑（支援儲存庫子目錄與 worktree），
不碰儲存庫其他檔案或暫存內容；推送 `git push` 的是分支歷史，無法限定單一檔案，建議獨立筆記儲存庫；
呼叫系統 git CLI（逾時 1 分鐘），macOS 墊片探測、非互動環境變數與「不做 pull/rebase/force push」
沿用原實作；設定失效、寫入保護或 `Close` 時按代號作廢在途任務並取消上下文；非快進等遠端拒絕推送回報 `ErrPushRejected` 並提示手動同步、解決衝突後再推送；`GitStatus` 發布
儲存庫根、最近提交/推送時間與錯誤，供「儲存與同步」頁展示。

### 雲端同步（sync.go，開關開啟後啟用）

每輪「先拉取 → 本地判定 → CAS 提交」（D12 / ADR-0004）：pull 讀取遠端內容與版本號（後端 A 為 blob sha）；
持鎖本地階段（順序 writeMu → gitGate → mu，與遷址、寫入保護互斥）先 flush 待儲存快照並 verifyDisk，
再按判定表處置：採用遠端（verifyDisk 通過才落盤，落盤後凍結儲存並請求介面重載，`Load` 解除）、
上傳本地（CAS 期望用本輪剛拉到的版本號，容忍同內容版本漂移）、只更新基線、或自動合併（雙方都改且
內容不同時：`sync_merge.go` 以基線原文為共同祖先按分頁 ID 三方合併，同一分頁兩邊都改按修改時間較新者
勝——本機檔案 mtime vs 遠端最近提交的 committer 時間，查不到遠端時間按本機較新；合併結果落盤、上傳並
提示已合併，不彈窗、不留檔；缺基線原文/解碼失敗/超分頁上限/回驗失敗時退化為整檔案較新者勝）。
從未同步過的裝置以雲端為準，歡迎文字不會覆蓋雲端筆記。CAS 被拒不盲重試舊版本號，重拉重判上限 3 輪。
基線（內容指紋 + 上次同步原文 + 後端版本號 + 本地/遠端存在性）存應用程式資料目錄 `sync-state.json`，
損毀時按「從未同步」降級（以雲端為準）。開啟同步經 `syncActive` 門禁暫停舊 Git 排程器（使用者偏好原樣保留），關閉後
按保留設定還原。同步權杖僅供後端驗證身分並持久儲存在系統安全儲存空間，不以明文寫入筆記、偏好或同步基線。

### 異常處理（無備份副本）

- 檔案不存在且未設定過 `storage.dataFile`：首次執行，寫入歡迎文字。
- 解析失敗 / 讀取失敗 / 版本過高 / 分頁超過 9 個：進入寫入保護，**保留原檔、拒絕寫入、不產生 `.pending` /
  `.corrupt` 副本**；使用者修復檔案後點「重新讀取檔案」（`reloadNotes`）顯式復原。
- 已設定的自訂路徑在重新啟動時遺失：同樣報錯並寫入保護，不當作首次啟動重建。
- 外部程式修改了檔案：儲存前指紋核對失敗，拒絕覆蓋；顯式重讀有效內容後繼續。

### hideOnEscape 缺鍵預設 true

設定裡其他項預設值都是 false/空字串，唯獨 `hideOnEscape` 預設 **true**。JSON 的 bool 分不清
「缺鍵」與顯式 `false`，故 `hasHideOnEscapeKey` 用 `map[string]json.RawMessage` 探測鍵是否存在：
缺鍵（含 settings 整體缺失）按 true 處理，顯式 false 保留。

## 資料檔案格式（`savedFile`）

```json
{
  "version": 3,
  "active": 0,
  "tabs": [{ "id": 1, "text": "…", "color": "#…" }],
  "settings": {
    "themeSource": "system",
    "alwaysOnTop": false,
    "hideOnEscape": true,
    "source": false,
    "tabMotionOff": false,
    "tabDotShape": "dot"
  }
}
```

| 欄位 | 類型 | 含義 |
|---|---|---|
| `version` | int | 資料版本，目前 `currentDataVersion = 3`；0/1 視為固定 7 點格式；更高拒載不覆蓋 |
| `active` | int | 目前啟用的分頁下標（載入時按分頁數 clamp） |
| `tabs` | [{id,name,text,color}] | 動態分頁（1–9 個）：穩定 ID、可選名稱、markdown 純文字、顏色（空/非法回退調色板）；v1 的 `dots` 欄位僅為讀取保留，新儲存不寫 |
| `settings.themeSource` | string | `system` \| `light` \| `dark`，空字串按 `system` |
| `settings.markdownTheme` | string | 空 / `sonnet` 為 Sonnet，`note` 為便箋（版本 2 文件裡的更名前舊 ID 隨載入移轉到新 ID） |
| `settings.bodyFont` / `settings.monoFont` | string | 正文 / 程式碼字型預設，空為主題預設 |
| `settings.bodySize` / `settings.lineHeight` | float32 | 正文字級 / 行高，0 為主題預設 |
| `settings.alwaysOnTop` | bool | 視窗置頂 |
| `settings.hideOnEscape` | bool | 按 Esc 隱藏視窗，**缺鍵預設 true**（見上） |
| `settings.source` | bool | 舊欄位僅讀取；原始碼/預覽切換是工作階段狀態，新儲存不再寫入 |
| `settings.preview` | bool | 舊版欄位，僅讀取時併入 `source`，不再寫出新值 |

## 偏好生命週期

`Run` 先呼叫 `resolveDataDir()` 解析資料目錄；失敗則不建構 Store，在 `WhenReady` 顯示原生錯誤後結束。
隨後 `loadPreferences` 讀取 `preferences.json`：存在則嚴格解碼（含舊版本欄位升級）；缺失時直接
寫入預設值——全新安裝不匯入舊 config 檔案。然後 `openConfiguredNotes` 按 `storage.dataFile`
開啟唯一筆記檔案（空字串 = 應用程式資料目錄 `zakki.json`）並 `load` 資料；設定路徑遺失時報錯並寫入保護。
執行期所有偏好變更走 `updatePreferences` 統一入口：驗證 → 套用執行期 → 同步選單/視窗/快速鍵 →
防抖自動儲存；磁碟寫經 `prefs.Store` 串行化、tmp/rename 原子替換，修訂號防舊快照覆蓋新值。
`preferences.json` 損毀或版本超前時進入待復原狀態：不覆蓋原檔案，設定頁提供「捨棄損壞的檔案並回復預設設定」（直接覆蓋原檔，不留 `.bad` 副本）；復原成功後目前工作階段立即按復原後的路徑重新載入筆記，只有載入成功才解除寫入保護。
偏好版本目前為 **16**：版本 16 新增不含憑證的 `ai` 節（API 金鑰只存系統安全儲存）。版本 15 新增 `sync.clientId`（GitHub App Client ID，非憑證；空字串回落環境變數 `ZAKKI_GITHUB_CLIENT_ID`，從 Finder 啟動拿不到 shell 環境變數時連接按鈕不再永遠點不動）；版本 14 新增雲端同步 `sync` 節（見「儲存與同步」雲端同步小節與 `internal/app/sync_bridge.go` 行）；版本 13 新增正文區左右/上下內距覆蓋；版本 12 由 `storage` 與 `git` 取代 `backup` 與防抖毫秒（見 `internal/prefs` 行）；
更早版本的鍵升級語意見 configuration.md。結束流程（`shutdownForQuit`）在筆記資料最終儲存後等待
偏好最終儲存，任一失敗阻止結束或更新重新啟動。完整契約見 [偏好與設定](configuration.md)；
MyGo 更新器另外管理 `updater.json`，不改變應用程式偏好的 JSON 契約。

## 測試接縫

- **路徑注入**：生產 `Run` 只呼叫一次 `resolveDataDir()`；解析結果傳給 `mygo.App.SetPath(PathUserData, dir)` 與 `newAppAt(dir)`。系統資料根目錄由 `mygoAppDataBase` 解析（macOS 為 `~/Library/Application Support/`，Windows 為 `%APPDATA%`）。
- **共享核心測試**：`core` 巢狀 module 的案例（權杖簇、`^` 規則、實例展開）經 `go -C core` 執行，已納入 `make test` / `make race`；架構檢查在 `make arch-check` 中驗證 core 的零依賴葉節點身分。
- **時鐘注入**：`app.now func() time.Time`，生產 `time.Now`，測試給假時鐘。
  建立後 `now` 只讀，背景可安全讀取。
- **`newTestApp(t)`**（`main_test.go`）：注入 `t.TempDir()` 與固定時鐘（2024-06-01），
  **絕不寫真實使用者目錄**。儲存、寫入保護與 Git 排程在 `internal/notes`；`persistence_test.go` 覆蓋載入映射與儲存語意。
- **原生生命週期回歸**：`ZAKKI_NATIVE_TEST=1 MYGO_LOCALE=zh-Hans go test ./internal/app -run '^TestNativeDockReopen$' -count=1 -v` 在 macOS 圖形工作階段中建置獨立 bundle、使用暫存資料目錄，驗證關閉重建、隱藏重用、最小化還原和可見時不重複開窗。預設 `MYGO_GPU=0` 隔離驅動初始化；顯式 `MYGO_GPU=1` 可複測 Metal 路徑，耗時與穩定性受驅動影響。
- **`ui.NewTester(a.view, w, h)`**：無視窗跑視圖（建構/斷言文字、模擬按鍵），
  `main_test.go`/`tab_style_test.go`/`live_boundary_test.go`/`live_focus_test.go` 用它。
- **偏好測試**：`internal/prefs` 的測試覆蓋預設值、嚴格解碼（已移除的舊 backup 欄位在相容版本中摘除）、損毀與超前版本復原、寫失敗回報、修訂號防回寫；筆記版本移轉表在 `internal/notes`；
  `menu_commands_test.go` / `internal/markdown` 的格式與行內邊界測試 / `view_theme_test.go`
  分別覆蓋加速器與衝突驗證、純格式轉換（含 CJK/emoji/游標映射）與全主題對比度；
  `menu_contract_test.go` 驗證新選單結構、設定視窗導覽（`ui.NewTester`）與改鍵流程。
- 測試指令見 `Makefile`：`make test`（`CGO_ENABLED=0 go test ./... -count=1`）、
  `make race`、`make vet`（含 `go tool mygo vet .` 的 UI 生命週期檢查）。
