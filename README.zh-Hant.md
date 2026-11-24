> *本文為繁體中文（台灣）譯本，內容以[中文原文](README.md)為準，可能落後。*

[中文](README.md) | [日本語](README.ja.md) | 繁體中文

# Zakki — 原生 Go 的 Markdown 速記便箋

Zakki 是一個 macOS / Windows 桌面速記應用程式：多個彩色分頁各存一段 Markdown，
Typora 式所見即所得編輯，純 [MyGo](https://github.com/egoist/mygo) 原生 Go UI
實作——無 HTML/JS/webview，單一二進位檔，GPU 繪製，介面語言內建英文、日語、
简体中文與繁體中文（預設跟隨系統）。

## 功能

- **彩色分頁速記**：預設 7 個分頁（可 1–9 個，與 `⌘1`–`⌘9` 一一對應）。未命名時每枚一樣大，當前只是顏色更深；
  有名字時，名字寫在當前分頁的色條裡；拖曳圓點即可調整順序。只有一個分頁時頂端列保持乾淨，只留關閉與置頂。
- **所見即所得編輯**：焦點區塊保留標題/段落/清單/引用排版，Markdown 標記只留在
  內部原始文字；表格帶懸浮工具列（調行列、對齊、刪除），待辦可直接勾選；
  `⌘⇧P` 隨時切換全文原始碼模式。
- **AI 助理（預設關閉）**：針對筆記提問，或產生可逐項確認的編輯建議。透過設定連接自己的 OpenAI 相容服務，並可限制只傳送目前分頁。[設定說明](docs/zh-Hant/configuration.md#ai-助理)
- **程式碼語法著色**：圍籬程式碼區塊按語言輕量著色（Go / JS / TS / Python / Shell / JSON /
  YAML / TOML / Rust / C / Java 家族），即時預覽與原始碼模式共用；外觀設定可選
  柔和 / 明快 / 跟隨文字。
- **長文件流暢**：預覽區塊清單只建構可見區塊，幾千個區塊的筆記捲動與按鍵依然輕快。
- **單一筆記檔案 + 自動儲存**：所有分頁存進一個 `zakki.json`，不再產生歷史副本；
  有修改按固定間隔（預設 2 秒）自動儲存，持續輸入不延後，`⌘S` 立即儲存，結束前最終儲存。
  檔案放進 git 儲存庫可開自動提交與推送（只提交這一個檔案）。
- **介面語言**：英文 / 日語 / 简体中文 / 繁體中文，跟隨系統，可在首次啟動精靈與設定隨時切換。
- **頂端列個人化**：切換動畫可關閉；分頁標記可選圓點 / 圓環。
- **原生體驗**：全域快速鍵喚出、視窗置頂、關窗常駐系統匣、單一實例、
  深淺色跟隨系統、可自訂的快速鍵支援錄鍵重新綁定（搜尋等固定快速鍵不可更改）。

## 安裝

從 [GitHub Releases](https://github.com/gomeen/Zakki/releases/latest) 下載：

- **macOS**：通用 DMG（Intel 與 Apple Silicon），拖入「應用程式」。目前為
  ad-hoc 簽章（無 Developer ID 與公證），首次開啟需在「系統設定 → 隱私與
  安全性」裡允許。
- **Windows 10/11 x64**：`zakki-setup-<版本>-amd64.exe`，按使用者安裝、無需管理員；
  未做 Authenticode 簽章，SmartScreen 可能提示，確認來源後執行。

應用程式內 **Zakki → 檢查更新…** 可手動檢查並安裝簽章更新（預設不背景檢查）。

## 快速鍵速覽

macOS 用左欄符號；Windows 把 ⌘ 換成 Ctrl、⌥ 換成 Alt、⇧ 換成 Shift。
兩平台的唯一差異是標題鍵：macOS 的標題用 ⌃（次級修飾鍵），Windows 用 Alt。

| 按鍵（macOS） | 功能 |
|---|---|
| `⌘1`–`⌘9` | 切換分頁（Windows：`Ctrl+1`–`Ctrl+9`） |
| `⌘T` / `⌘R` | 新建 / 重新命名分頁 |
| `⌘⌥←` / `⌘⌥→` | 上一個 / 下一個分頁 |
| `⌘B` / `⌘I` / `⌘K` | 粗體 / 斜體 / 連結 |
| `⌃1`–`⌃6` / `⌃0` | 標題 1–6 / 正文（Windows：`Alt+1`–`Alt+6` / `Alt+0`） |
| `⌘⇧8` / `⌘⇧7` / `⌘⇧9` | 無序 / 有序 / 任務清單 |
| `⌘⇧Q` / `⌘⌥C` | 引用 / 程式碼區塊 |
| `⌘F` / `⌘G` / `⇧⌘G` | 搜尋 / 下一個 / 上一個（搜尋框內 `Enter` / `⇧Enter`） |
| `⌥⌘F` / `⇧⌘F` | 搜尋與取代 / 在所有分頁中搜尋 |
| `⌘⇧P` | 原始碼模式 |
| `⌘S` | 立即儲存 |
| `⌘⇧C` | 複製分頁全文（檔案選單 / 分頁右鍵還可「匯出為 Markdown…」） |
| `⌘⇧空格` | 全域顯示/隱藏（可改鍵） |
| `⌥⌘T` | 視窗置頂 |
| `⌘=` / `⌘-` / `⌘0` | 放大 / 縮小 / 預設字級 |
| `⌘,` | 設定 |

Markdown 指令與全域喚出都可在 **設定 → 快速鍵** 裡錄製改鍵。

## 資料與儲存

- 資料目錄：macOS `~/Library/Application Support/Zakki/`，
  Windows `%APPDATA%\Zakki\`。
- 唯一的筆記檔案 `zakki.json` 儲存全部分頁文字和部分分頁列設定，透過 tmp+rename 原子寫入。
  應用程式偏好另外儲存在 `preferences.json`。
- 首次啟動精靈可選擇儲存位置（任意資料夾，含 git 儲存庫）；之後「設定 → 儲存與同步」
  可隨時更換（拒絕覆蓋既有檔案，失敗保留原檔）。
- 檔案損毀或被外部改動時保留原檔、暫停寫入，提供「重新讀取檔案」顯式復原；
  結束前最終儲存失敗會阻止結束並說明原因。

## 開發

```sh
go tool mygo dev      # 開發（熱重載）
make test             # 全量測試（含 MyGo vendor 補丁包）
make check            # gofmt + vet（含 mygo vet）+ test + race + arch 檢查（CI 同款）
make build            # macOS 通用 .app / .dmg
make build-windows    # Windows x64 .exe
```

介面文案在 `locales/*.json`（ICU MessageFormat，ARB 格式）；
`go tool mygo generate` 產生 `locales/catalog_gen.go`，鍵位一致性有測試保障。
工程標準與模組職責見 [docs/architecture.md](docs/zh-Hant/architecture.md)。

## 效能

編輯與預覽採用區塊級虛擬清單，長文件的影格成本主要隨可見區塊數成長。歷史量測與未解瓶頸見 [docs/reports/](docs/reports/)（中文）。

## 授權

[MIT](LICENSE)。內嵌資源各自遵循其授權條款：Lucide 圖示（ISC）。霞鶩文楷 Lite、JetBrains Mono
與 Fira Code 字型（均為 OFL）不隨安裝包散布，應用程式內在首次選擇時從各自官方 GitHub 儲存庫
下載（見 `internal/fonts/catalog.go`），授權條款文字保留在 `resources/OFL-*.txt`。
