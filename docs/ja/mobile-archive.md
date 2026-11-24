> *本文書は日本語訳です。内容は[中国語の原文](../mobile-archive.md)を正とし、翻訳が遅れている場合があります。*

# モバイルクライアントのアーカイブ：進捗、未完了項目、復帰の道筋

> **状態：停止。開発は継続しない（2026-11-11 の決定。[ADR-0003](../adr/0003-pause-mobile-client.md)（中国語）を参照）。**
> 目標仕様書は削除済み（履歴は git 履歴を参照）。コードは `mobile/` に残し、凍結のみで削除はしない。
> 本文は 4 つの問いに答える：どこまで進んだか、何が足りないか、当初何を作るつもりだったか、
> 将来再開するときにどう再開するか。

## 1. なぜ止めたか

- 無料の Personal Team 署名しかない：profile は 7 日で失効、デバイスあたり 3 つの App ID、Time Sensitive entitlement は使えない（spec
  §10 D19。M3 出口の A2 項目はこれで検証できない）。
- 端末にインストール済みのアプリは署名失効により**すでに使えない**。再署名の手順は、付箋のような高頻度ツールとしては受け入れられない。
- 残りの作業（§3.1 の全 11 項目 + M5）は**ほぼすべて実機検収**で、有料アカウントなしには完結できない。コードを書き続けても、自分で使えない実装が残るだけになる。

## 2. 完了したもの（コード上の事実であり、完了宣言ではない）

### 2.1 マイルストーン

| マイルストーン | 成果物 | チケット | 検証記録 |
|---|---|---|---|
| M1 スキャフォールド | 同リポジトリの `mobile/binding`（独立 module、`replace => ../../core`、`golang.org/x/mobile` を唯一許可）+ XcodeGen プロジェクト + `make mobile-framework` + `mobile.yml` CI + ファイル選択による `zakki.json` のインポート；高バージョンの書き込み保護、未知フィールドの入れ子往復、9 タブ超過でもデータを失わない | [#25](https://github.com/gomeen/Zakki/pull/25) | [2026-10-28](../reports/2026-10-28-m1-scaffold-verification.md)（中国語） |
| M2 エディタ | SwiftUI の殻 + UIKit テキストスタックの 2 モード（リアルタイム描画 / ソース）、トークン群の往復と書き戻し、Markdown クイックバー、最小テーマセットとドリフト防止の同期、タブ管理のバインディング、システム共有による書き出し | [#31](https://github.com/gomeen/Zakki/pull/31)–[#34](https://github.com/gomeen/Zakki/pull/34)、[#37](https://github.com/gomeen/Zakki/pull/37) | [2026-10-29](../reports/2026-10-29-m2-editor-verification.md)（中国語）（126 件の XCTest）|
| M3 iOS リマインダー | トークンのポップオーバー、単発通知の並べ直し、完了の前進、繰り返しの展開、タスクを横断する 64 スロットを最も早い `fireAt` で割当 + 最終スロットの継続スケジュール、状態表示（スケジュール済み/同期待ち/継続待ち/権限制限による降格）| [#51](https://github.com/gomeen/Zakki/issues/51) | [2026-11-08](../reports/2026-11-08-m3-ios-reminders-verification.md)（中国語）（53 件の新規 XCTest、シミュレータ scheduled=63+renewal）|
| M4-1 同期エンジン + GitHub バックエンド A | §2.2 判定表の 4 ケース、sha CAS（409/422 で再取得して再判定）、Device Flow + 更新 + 失効、Keychain、`conflicts/` アーカイブ、衝突ダイアログ | [#52](https://github.com/gomeen/Zakki/issues/52) | [2026-11-08](../reports/2026-11-08-m4-1-sync-verification.md)（中国語）（46 件の新規 XCTest + プロセス内 GitHub stub のエンドツーエンド）|
| M4-2 iCloud クラウドドライブ バックエンド B | ファイル選択の認可 + security-scoped bookmark、ベースライン = ファイル同一性 + mtime の書き込み前照合、命名スキャン + NSFileVersion の 2 経路の衝突コピー、ブックマーク失効時の再選択状態機械 | [#53](https://github.com/gomeen/Zakki/issues/53) | [2026-11-08](../reports/2026-11-08-m4-2-icloud-backend-verification.md)（中国語）（35 件の新規 XCTest + 3 シナリオ + コンテナをまたぐ実 picker）|
| iCloud プローブ（D21 ゲート）| シミュレータでは entitlement なしのファイル選択、再起動後のブックマーク復元とローカルファイルの直接読み書き、プレースホルダ判定と衝突スキャンのローカルでの挙動を確認。実際の iCloud Drive、プレースホルダのダウンロード、衝突検出は実機検証が必要 | — | [2026-11-08](../reports/2026-11-08-icloud-sync-probe.md)（中国語） |

### 2.2 M4-3 デスクトップ同期の付帯：**デスクトップに属し、モバイルの凍結に含まれない**

この節はアーカイブで巻き添えになりやすいので別に説明します：同期の環境設定節（prefs v14）、旧 Git スケジューラの一時停止/復元、先に取得してからコミットする同期ループは、
いずれも**デスクトップの実装**であり、0.6.3 でリリース済みです（`f361012`、
検証記録 [2026-11-08](../reports/2026-11-08-m4-3-desktop-sync-verification.md)（中国語））。
凍結されたのは iOS 側の同じエンジンで、デスクトップ側は通常どおり保守します。

注意：
デスクトップ側はその後 [ADR-0004 で D13 の衝突ダイアログと `conflicts/` アーカイブを廃止しました](../adr/0004-sync-auto-merge-no-archives.md)
（中国語）——双方が変更したときはタブ単位で自動マージし、ダイアログもアーカイブも残しません。本文 §3.1 で「D13／アーカイブ／衝突ダイアログ」とある検収項目は凍結された iOS 側にのみ適用さ
れ、デスクトップ側の対応項目は ADR-0004 を正とします。

### 2.3 コードとテストの規模

| 場所 | 内容 | 規模 |
|---|---|---|
| `mobile/ios/Zakki` | SwiftUI の殻、エディタ、リマインダー、同期、設定 | 73 個の Swift ファイル / 約 17340 行 |
| `mobile/ios/ZakkiTests` | 単体テストと契約テスト | 331 件の XCTest |
| `mobile/ios/ZakkiUITests` | UI のエンドツーエンド（CI には入らず、中国語環境と座標タップに依存）| 10 件の XCTest |
| `mobile/binding` | gomobile バインディングのビルド module | 2 個の Go ファイル（`deps.go`、`doc.go`）+ `go.mod` の tool directive |
| `.github/workflows/mobile.yml` | バインディング成果物 + プロジェクト生成の再現性 | 1 本の workflow |

## 3. 未完了

### 3.1 コードは完了し、実機/人手の検証だけが残る（11 項目）

すべて元の目標仕様 §8 の未チェック行です（仕様書は削除済み、履歴は git 履歴を参照）。「再開時にまず何をするか」の順に並べます：

**第 1 優先（路線全体が成立するかを決める）**

| 項目 | 内容 | なぜ優先か |
|---|---|---|
| C1 | 実機の picker で iCloud Drive のノードが見えて選択できる | 失敗すると D21 の規定によりバックエンド B は M5 に後退し、路線の変更が必要 |
| C4 | プレースホルダファイルのダウンロードポーリング（`notDownloaded` → `.current` → 全文読み込み）| `PlaceholderReader` のプレースホルダ分岐は**一度も実行されたことがなく**、コンパイルが通っただけ |

**次に（リマインダーの可用性）**

| 項目 | 内容 |
|---|---|
| A1 | 無料の Personal Team での Time Sensitive 降格の確認 |
| A2 | 有料アカウント + `Zakki.entitlements` での Time Sensitive 集中モード貫通（**停止の直接の原因**）|
| A3 | 到点発火のマトリクス：ロック画面 / バックグラウンド / 集中モード / スリープ復帰 / 手動の時刻変更 / タイムゾーン切り替え |
| A4 | 無料アカウントの 7 日ごとの再署名手順（**停止の直接の原因**）|
| A5–A7 | 同一 bundle への上書き再署名 / 上書きインストール / アンインストールして再インストール。3 つの場合でデータ、認可、pending をそれぞれ確認する（相互に推論してはならない）|
| A8 | App ID のデバイスあたりの割当（7 日ごとに 10 個）|
| A9 | 低電力モードでの状態バーの更新タイミング |
| A10 | profile 失効後の検出と通知 |

**その次に（同期）**

| 項目 | 内容 |
|---|---|
| B1–B3 | GitHub App の登録 → Device Flow の実接続 → token の実際の失効（8h）とサイレント更新 → GitHub 側で認可を取り消した後の切断と再接続 |
| B4 | セルラーの実機で `api.github.com` に到達できるか |
| B5 | 無料署名での上書きインストール/再署名の後に Keychain の資格情報が存続するか |
| B6 | レート制限と大ファイル（>1MB の raw フォールバック）のオンラインでの挙動——現状は httptest の mock のみ |
| C2–C3 | その場での書き戻しが Mac 側で見える、未認可の直接読み取りが拒否される + 認可取り消し後に `access-denied` を報告する |
| C5 | 実際の衝突コピーの 2 経路検出（`zakki (N).json` / conflicted copy / NSFileVersion）|
| E1–E4 | **デスクトップ ↔ iPhone の双方向連携**（M4 出口の最後の環）、両端での実際の sha CAS 衝突、判定表 4 ケースの再現、旧 Git スケジューラの一時停止と復元（デスクトップ側の衝突は自動マージに変わったため、連携テストは「マージ結果が見える、二重書き込みがない」の検証に変更）|

**人手の手触り**（[#26](https://github.com/gomeen/Zakki/issues/26)）

自分の `zakki.json` のコピーをインポート、長文の中国語を大量に編集（長文 + 多数のコンポーネント +中英混在 + 取り消し/クリップボード/IME の変換中文字）、
リアルタイムスタイルの目視（見出し/太字斜体/タスク/引用/コードブロック/時間コンポーネント × ライト × ダーク × 横向き）、書き込み保護されたファイルが読み取り専用のままであること、
書き出し → デスクトップで再保存しても未知フィールドが失われないこと、横向きのソフトキーボードとドラッグ並べ替えの手触り。

### 3.2 まったく未実装

| 範囲 | 内容 | 備考 |
|---|---|---|
| M5 Android | Android プロジェクト / Kotlin + Compose エディタ / AAR のビルドとロード / AlarmManager のアラーム（`setAlarmClock`、正確な権限、Doze）/ BOOT_COMPLETED などによるコールドスタート再構築 / タイムゾーン注入 / 両端のウィジェット | `mobile/` に Android のディレクトリは一切なく、1 行も書かれていない |
| M5 権限とライフサイクル | Android 12+ の正確な権限の拒否と降格、API 33+ の `USE_EXACT_ALARM`、`POST_NOTIFICATIONS` の拒否、通知チャネルのオフ、force-stop からの復帰 | 同上 |
| M5 配布 | 署名、配布フロー、App Group / WidgetKit capability、16KB ページサイズの AAR | 同上 |
| M5 クラウドドライブの補完 | OneDrive 中国版の到達性、iCloud とサードパーティ provider の差異、NSFileVersion の復帰入口、2 台目のデスクトップが既存ファイルに参加（spec R8）| 同上 |
| #57 | `ReminderFlowUITests.testPopoverClearRemovesToken` が main 上で恒常的に失敗 | コードレベルの問題で環境の問題ではない；再開時はまずこれを直さないと UI テストのベースラインが赤い |
| #19 | Android FCM のフォールバック層の研究 | もともと v1 以降に延期されており、モバイルと一緒にアーカイブ |

## 4. 目標機能（当初何を作るつもりだったか）

仕様書は削除済みで、完全な定義は git 履歴の `docs/mobile-spec.md` にあります。ここには索引だけ残します：

| 欲しかったもの | 仕様の位置 |
|---|---|
| v1 MVP の範囲：閲覧、重量級の編集（リアルタイム/ソース）、タブ、設定とリマインダーの配信、繰り返しの全機能、差し替え可能な 2 バックエンド同期、今日が期限のウィジェット | §5 |
| 5 つのマイルストーンの出口基準（M1 スキャフォールド → M5 Android と締めくくり）| §7 |
| 2 バックエンド同期プロトコル（GitHub A / クラウドドライブ B）、衝突判定とアーカイブ（iOS 側；デスクトップ側は自動マージに変更、ADR-0004 を参照）| §2.2、§10 D13 |
| 時間とリマインダーの共有契約：バインディング API、スケジューリングのメインループ、通知の表示 | §3 |
| 決定済みの事項：D11 FCM なし、D13 衝突は LWW にしない、D17 コンポーネントのインタラクション、D19 無料署名の境界、D21 マイルストーンの編成 | §10 |
| 視覚のベースライン | 旧 `docs/design-tokens-mobile-draft.md`（削除済み、履歴は git 履歴を参照）|

**明確に範囲に含めたことのないもの**（負債ではない）：タブ横断検索、通知のディープリンク、アプリ内の今日の集約ページ（この 3 つは fast-follow）、iPad 専用レイアウト、ウォッチ、
Siri/ショートカット、ベンダープッシュチャネル、ストア掲載/TestFlight、堅果雲 WebDAV、iCloud for Windows、CloudKit、Syncthing、
自前の同期サービス、Dropbox（§0、§2.2 の最終行）。

## 5. 復帰の道筋

**前提条件（2 択、第 3 の道はない）**

- 有料の Apple Developer アカウントを開設する：Time Sensitive entitlement が使え、署名が 1 年有効；
- または「7 日ごとの再署名、自分専用、長期の可用性は約束しない」を明確に受け入れる。その場合 A2 と A4 は適用外とし、M3 の出口は「再署名した当日だけ使える」に降格する。

**順序（段を飛ばさない）**

1. `make mobile-framework` + `mobile.yml` をまず緑にし、凍結期間中の `core/` の変更がバインディングのコンパイルを壊していないことを確認する；
2. 実機で **C1 + C4** を検証する。この 2 項目が D21 の路線がまだ成立するかを決める；失敗したら #53 を閉じ、バックエンド B は M5 に後退する；
3. 実機で A1、A3、A5–A10 を検証し（A2 はアカウント次第）、M3 の出口を締める；
4. 実機で B1–B6 を検証する（GitHub App の登録 + Device Flow + Keychain）；
5. 最後に **E1–E4 の両端連携**を行う。これが M4 出口の最後の環；
6. その後で初めて #26 の人手の手触りと M5 に移る。

**入口の早見表**

| 探したいもの | 行き先 |
|---|---|
| 目標と出口基準 | 旧 `docs/mobile-spec.md` §7、§8（削除済み、履歴は git 履歴を参照）|
| 各マイルストーンの実装詳細と証拠 | `docs/reports/2026-10-2x-m1/m2…`、`2026-11-08-m3/m4-1/m4-2/m4-3…`、`2026-11-08-icloud-sync-probe.md` |
| 実機の runbook | `2026-11-08-icloud-sync-probe.md` 付録 A |
| バインディングのビルド | `Makefile` の `mobile-framework`、`mobile/binding/go.mod` |
| iOS プロジェクト | `mobile/ios/project.yml`（XcodeGen が `.xcodeproj` を生成、生成物はコミットしない）|

## 6. 凍結期間の取り決め

- iOS のバグを直さない、iOS の機能を追加しない、実機検収を行わない；`mobile/` と `mobile.yml` はそのまま残す。
- `core/` は**凍結しない**：デスクトップが消費している。`core/` を変更するときは `make test`（ルートでの `go test ./...` はネストされたモジュールの core 契約テストを実行しない）を必ず実行し、
  `mobile/binding` が依存する `zakkicore` のエクスポート面に注意する。
- デスクトップ側で `zakki.json` の形式や移行の意味論を変える場合も、仕様 §2.1 の共有契約に拘束される（ADR-0003 第 5 条）。
- issue [#26](https://github.com/gomeen/Zakki/issues/26)、
  [#50](https://github.com/gomeen/Zakki/issues/50)、[#57](https://github.com/gomeen/Zakki/issues/57)、
  [#19](https://github.com/gomeen/Zakki/issues/19) は再開の入口として open のまま保ち、triage キューには入れない。

## 7. 凍結時点のデスクトップ残作業

以下は凍結時点の記録であり、現在の進捗ではありません。現在のデスクトップの残作業は [`.scratch/`](../../.scratch/) を参照してください。モバイルの凍結範囲は変わりません。

| 待ち項目 | 現状 | 関与範囲 |
|---|---|---|
| [#22](https://github.com/gomeen/Zakki/issues/22) Windows 通知の実機 + macOS 通知センターの視覚確認 | チェックリストは作成済み（`docs/verification/notifications-manual-checklist.md`）、パラメータ/スクリプトの単体テストとクロスコンパイルの証拠のみ | 実機の Windows 1 台 + macOS が必要 |
| デスクトップ同期の実アカウント検証 | デスクトップの GitHub バックエンド A はリリース済みだが、Device Flow、token の失効/取り消し、レート制限、>1MB の raw フォールバックは mock の証拠しかない | 純粋にデスクトップ、GitHub App 1 つで開始できる |
| Windows DPAPI の安全なストレージ | クロスコンパイル + インターフェーステストは通過、**実際の Windows では一度も実行していない** | 純粋にデスクトップ、Windows 1 台が必要 |
| Copilot 風の入力補完 | 調査レポートは完成（[リポジトリ収録済み](../reports/2026-11-11-copilot-style-input-research.md)・中国語）、MVP の提案はレポート §6.4；根本的な欠落は MyGo に ghost text の装飾層がないこと | 純粋にデスクトップ、まず意思決定が必要 |
| [#49](https://github.com/gomeen/Zakki/issues/49) Awesome MyGo への掲載 | 対外的なリストへの登録で、コードではない | メタデータ |
| パフォーマンスと起動に関する今後の提案 | `docs/reports/2026-10-18-performance-review.md`、`2026-10-18-startup-shutdown-review.md` の「今後の提案」を参照：大きな表のブロック内仮想化、ミラー取得と Git runner のタイムアウト、バックアップ同期入口のバックグラウンド化、移行時のメタデータ事前検査 | 純粋にデスクトップ、いずれも非ブロッキング |
