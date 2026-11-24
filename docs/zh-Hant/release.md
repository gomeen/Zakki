> *本文為繁體中文（台灣）譯本，內容以[中文原文](../release.md)為準，可能落後。*

# Zakki 發布與更新

## 目前方案

- 儲存庫與更新分發：公開儲存庫 `gomeen/Zakki` 的 GitHub Releases，無需額外伺服器。
- 應用程式名與安裝包：macOS 為 `Zakki.app` / DMG，Windows 為 `Zakki.exe` / Setup EXE。
  應用程式識別碼 `com.gomeen.zakki`，Go module 名 `zakki`；資料目錄 macOS 為
  `~/Library/Application Support/Zakki/`，Windows 為 `%APPDATA%\Zakki\`，不改變 JSON 結構。
  應用程式按全新安裝處理，不做舊資料目錄移轉。
- 更新器：由 [`go.mod`](../../go.mod) 鎖定的 MyGo 提供的 `plugins/updater/native`，全原生 UI，無 webview；應用程式選單與系統匣
  均提供「檢查更新…」，預設只手動檢查。使用者可檢視版本說明、下載安裝、略過或稍後重新啟動。
- 發布目標固定為 **darwin/universal**（Intel 與 Apple Silicon）和 **windows/amd64**（Windows 10/11 x64）。
  同一個 Release 分別提供 `update-darwin-universal.json` 與 `update-windows-amd64.json`，兩種系統不會混用更新。
  每種架構獨立訂閱清單，後續不要隨意切換目標，否則既有版本會收不到更新；目前不發布 Windows ARM64 原生包。
- Release 附件：兩個安裝包、兩個簽章更新壓縮檔、兩個更新清單，以及從目前 Latest 到
  新版本的簽章增量包（`*.delta`）；不產生 SHA256SUMS（完整性由清單內的 Ed25519
  簽章保證，驗證失敗不寫入應用程式）。`mygo.json` 的 `updates.deltas` 為 `1`：
  只為目前 Latest 產生到新版本的增量包，更早版本直接下載完整壓縮檔。應用程式優先使用匹配 delta，補丁缺失、
  無體積優勢、驗證失敗或本地應用程式被修改時自動回退完整簽章壓縮檔，兜底不能關閉。
- **沒有 Apple 開發者帳號**：`macos.signingIdentity` 為 `-`，僅 ad-hoc 簽章，不做公證。
  Windows 尚無 Authenticode 程式碼簽章，SmartScreen 可能顯示未知發行者。
  兩個平台共用 Ed25519 更新簽章金鑰；它驗證更新包來源，不代替 Apple 或 Windows 的系統程式碼簽章。

`.github/workflows/ci.yml` 檢查分支與 PR；`release.yml` 在推送 `vX.Y.Z` tag 後檢查、封裝，
先全量檢查，再分別在 macOS 與 Ubuntu runner 建置兩個平台；**兩個平台都成功**後，統一上傳到
**草稿 Release**，不自動公開。任一平台失敗時不執行上傳 job，另一個成功平台的 Actions 附件仍可用於排查。
設定到位不代表已經發布：仍需設定 Secret、推送版本並公開草稿。

## 首次設定（只需一次）

### 1. 更新簽章金鑰

本專案的公鑰已寫入 `mygo.json`。對應私鑰在設定時產生，保存在儲存庫外：

```text
~/Library/Application Support/mygo/update-keys/zakki/mygo-update.key
~/Library/Application Support/mygo/update-keys/zakki/mygo-update.pub
```

私鑰權限為 `0600`。將私鑰備份到密碼管理器或其他安全位置；不要貼到聊天、提交到 Git、
上傳到 Release，也不要在紀錄中輸出。遺失私鑰後不能給已安裝版本發布可信更新；洩露則可能被用來偽造更新。
不要重新執行 keygen 替換既有金鑰。

在完成 GitHub CLI 登入後，將**私鑰檔案內容**寫入儲存庫 Actions Secret（不是檔案路徑）：

```sh
gh auth login
gh secret set MYGO_UPDATER_PRIVATE_KEY --repo gomeen/Zakki \
  < "$HOME/Library/Application Support/mygo/update-keys/zakki/mygo-update.key"
```

本地設定階段不執行上述 GitHub 寫入。發布工作流使用 GitHub 自帶的 `github.token` 上傳，
不需要額外 PAT，更不能把 Token 內建進應用程式。確認儲存庫允許 Actions 使用工作流宣告的 `contents: write` 權限。
如果原始碼儲存庫改為私有，應另外提供公開發布儲存庫並同步 `updates.github`；目前更新器不攜帶私有儲存庫憑證。

### 2. 首次提交與遠端

遠端應為 `git@github.com:gomeen/Zakki.git`，分支為 `main`。
設定工作不會自動提交、打 tag 或推送；審核改動並確認工作區中的其他修改後，再自行提交推送。

## 每次發布

1. 修改 **`mygo.json` 的 `version`**（唯一版本來源），並為 `CHANGELOG.md` 增加 `## X.Y.Z` 章節。
   應用程式內更新說明來自該章節；檔案存在但章節缺失時建置會失敗。
2. 依下述工作流程的規則驗證根模組與 `core` 並執行 `make check`，同步 README 與行為變化說明。
3. 提交程式碼，確認待發布提交無遺漏，然後打同號 tag。以下指令屬於主動發布操作，設定階段不要執行：

   ```sh
   version="$(jq -r .version mygo.json)"
   git push origin main
   git tag "v$version"
   git push origin "v$version"
   ```

4. 檢視 Actions 的 `release` 工作流。它會：
   - 核對 tag、版本、儲存庫與私鑰 Secret，並要求新版本嚴格高於目前 Latest；缺私鑰或版本倒退直接失敗，不產生不完整的發行包。
   - 在根目錄執行 `go mod verify`，只豁免本機目錄替換（`replace zakki/core => ./core`）的已知 `missing ziphash` 誤報；其餘驗證失敗均阻擋發布。接著執行 `go -C core mod verify` 和完整 `make check`，兩者皆須通過。
   - macOS runner 建置 universal 包，Ubuntu runner 安裝 NSIS 並交叉建置 Windows x64 安裝器。
   - 檢查安裝器、清單版本/URL、壓縮檔大小；額外核對 macOS 雙架構與 ad-hoc 簽章、Windows PE x64 可執行檔。
   - 讀取目前 Latest 版本，要求兩個平台的清單和附件均包含該版本到新版本的有效 delta，並核對簽章欄位與實際檔案大小。
   - 保留兩個平台的 Actions 附件；全部成功後彙總、再次驗證，再上傳至草稿 Release，兩個更新清單最後上傳。
   - 拒絕覆蓋已公開的同號 Release；修復已公開版本必須遞增版本號。
5. 從草稿下載測試，在另一台 Mac/乾淨帳號與真實 Windows 10/11 x64 上檢查安裝、系統匣、全域快速鍵、中文輸入法、儲存與重新啟動。
   交叉編譯不等於完成 Windows 執行驗收。首次接入更新器的版本需要手動安裝；之後兩個平台各做一次「已安裝舊版 → 新版」的真實更新回歸。
6. 確認草稿包含下列全部檔案，在 GitHub 點擊 **Publish release**，確保設為 **Latest**，不要設成 prerelease。
   公開後，使用者的「檢查更新」才會看到該版本。Release 網頁說明可在草稿中編輯，應用程式內說明來自 CHANGELOG。

## 建置與產物

一般本地建置（不上傳）：

```sh
make build            # macOS universal
make build-windows    # Windows x64
```

macOS 產生 Windows 安裝器需先安裝 NSIS（`brew install nsis`），Linux 為 `sudo apt-get install nsis`；
在 Windows 上執行 MyGo CLI，缺 NSIS 時會下載並驗證其工具鏈。只需要原生 UI，無需 WebView2。
本地沒有 NSIS 時 MyGo 會警告並略過安裝器，但仍可交叉產生 EXE 與簽章更新檔案；發布工作流強制要求安裝器存在。

帶更新簽章的本地建置（同樣不上傳，Windows 將最後一行換成 `make build-windows`）：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(< "$HOME/Library/Application Support/mygo/update-keys/zakki/mygo-update.key")" \
  make build
```

`build/darwin-universal/` 中的檔案：

| 檔案 | 用途 |
|---|---|
| `Zakki.app` | 本地執行的應用程式包，不直接上傳 Release |
| `zakki-<version>.dmg` | 使用者首次安裝或手動更新 |
| `zakki-<version>-darwin-universal.tar.gz` | 應用程式內更新所下載的簽章壓縮檔 |
| `update-darwin-universal.json` | 版本、說明、URL、大小、簽章等更新清單 |
| `*.delta`（有可用舊版本時） | 從上一 Latest 到新版本的增量更新，失敗自動回退完整壓縮檔 |

`build/windows-amd64/` 中的檔案：

| 檔案 | 用途 |
|---|---|
| `Zakki.exe` 與資源檔案 | 本地執行，Release 以安裝器分發，不單獨上傳裸 EXE |
| `zakki-setup-<version>-amd64.exe` | 按使用者安裝器；預設安裝到 `%LOCALAPPDATA%\Programs\Zakki`，無需管理員權限 |
| `zakki-<version>-windows-amd64.tar.gz` | Windows 應用程式內更新的簽章壓縮檔，不含 Setup 與解除安裝器 |
| `update-windows-amd64.json` | Windows x64 的版本、說明、下載資訊與簽章 |
| `*.delta`（有可用舊版本時） | Windows 增量更新，失敗自動回退完整壓縮檔 |

增量包以「目前 Latest → 新版本」為基線；應用程式優先使用匹配 delta，但補丁缺失、
無體積優勢、驗證失敗或本地應用程式被修改時，會自動回退完整簽章壓縮檔，這一安全兜底
不能關閉。下載與解包都以更新清單中的 Ed25519 簽章驗證，驗證失敗時放棄安裝並提示。

MyGo 缺私鑰時只警告並略過 macOS/Windows 更新檔案，所以本地一般建置仍可用，但不能拿其安裝包當作完整的更新發布。
工作流會檢查私鑰與產物，公鑰私鑰不匹配時建置失敗。產生 DMG 需要 macOS；Windows 可從 macOS/Linux 交叉建置，安裝器需要 NSIS。

更新清單入口：

```text
https://github.com/gomeen/Zakki/releases/latest/download/update-darwin-universal.json
https://github.com/gomeen/Zakki/releases/latest/download/update-windows-amd64.json
```

只上傳原始碼或打 tag、只有 DMG、草稿未公開、未設 Latest，均不能形成可用的正式更新。
目前設定不會檢查 prerelease，也不要用正式渠道的 tag 測試預發布版本。

## 使用者安裝與更新

- **macOS 首次安裝**：下載 DMG，把 `Zakki.app` 拖到 `/Applications` 或自己的 `~/Applications`。
  不能直接在唯讀 DMG 內自更新；安裝目錄必須可寫。開發模式也不能自更新。
- **Windows 首次安裝**：下載 `zakki-setup-<version>-amd64.exe`，按使用者安裝到預設目錄。
  自訂目錄也必須可寫，不建議裝入需要管理員權限的 Program Files。
  資料位於 `%APPDATA%\Zakki\`，不隨應用程式更新替換；不要把資料目錄選成程式安裝目錄（解除安裝器會刪除安裝目錄）。
- **更早版本使用者**：先儲存並完整結束舊版，再單獨備份舊版資料。Zakki 不會掃描或自動移轉更名前的舊資料目錄；安裝新版後應確認使用的是預期資料檔案，且不要同時啟動兩版寫同一份資料。
- **之後更新**：Zakki → 檢查更新… → 檢視說明 → 下載並安裝 → 重新啟動。
  更新替換應用程式包，不刪除目前版本的筆記和偏好；重新啟動經過結束最終儲存，儲存失敗會取消結束並顯示錯誤。
- **Windows 安全提示**：目前未設定 Authenticode，可能出現 SmartScreen/未知發行者提示；確認下載來源後再決定是否執行。
  之後可另行設定 Windows 程式碼簽章憑證，與 Apple 開發者帳號及既有更新金鑰獨立。
- **網路或權限問題**：更新視窗顯示失敗或不可用；排查 GitHub 存取、安裝目錄寫入權限，必要時完整結束後重新執行新版安裝器或覆蓋應用程式。
- **macOS 安全提示**：目前未獲 Developer ID 信任與公證，首次開啟可能被 Gatekeeper 阻擋。
  確認來源後透過系統「隱私與安全性」允許開啟；不要全域關閉 Gatekeeper。
  更新簽章不保證消除系統攔截，正式對外發布前仍建議辦理開發者帳號並補齊簽章、公證。

## 發布驗收

- [ ] `mygo.json` 與 tag 同號，CHANGELOG 有對應章節，根模組驗證符合上述規則，且 `go -C core mod verify` 與 `make check` 通過。
- [ ] Secret 對應目前公鑰，私鑰有獨立安全備份，Git 與附件中均無私鑰。
- [ ] macOS 與 Windows 建置 job 均成功，草稿包含 DMG、Setup EXE、兩份簽章壓縮檔、兩份更新清單與兩平台的 delta，無多餘附件。
- [ ] Intel / Apple Silicon 安裝可啟動；對真實下載檔案驗證 Gatekeeper 行為，不以本機無隔離標記的包代替。
- [ ] Windows 10/11 x64 按使用者安裝、啟動、中文輸入法、系統匣、快速鍵與結束通過實機驗收，確認 SmartScreen 提示；不能僅憑交叉編譯宣布已驗證。
- [ ] 舊版檢查更新可下載安裝並重新啟動；兩個平台的清單均包含從上一 Latest 版本出發的 delta，附件存在且大小一致。
- [ ] 更新前後筆記與偏好保留；儲存失敗不靜默重新啟動。
- [ ] 公開 Release 並設為 Latest 後，清單和壓縮檔均可匿名下載。
