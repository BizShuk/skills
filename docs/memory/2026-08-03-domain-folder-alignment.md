# 2026-08-03 — 目錄範疇對齊 (Domain Folder Alignment)

## 起因 (Trigger)

全專案審視「每個資料夾的內容是否對得上它宣稱的領域」。起點是 `utils/walk.go`：
一個 308 行、import 了 `svc/fetch` 與 `svc/plugin` 的 BFS 走訪器。

## 發現 (Findings)

### 反向依賴 (Inverted dependency)

`utils/` 應該是 leaf layer，卻 import 了 `svc/*`。結果是
`svc/agent → utils → svc/plugin → svc/fetch` —— `svc/agent` 只是想用
`CopyTree`，卻被迫傳遞依賴到整個 plugin 解析鏈。

`README.md` 早就寫著「走訪位於 `svc/discover/discover.go`」，但那個路徑`不存在`。
文件記錄的是`意圖`，程式碼漂移到了 `utils/`。文件與程式碼分岔時，
先查哪一邊才是設計意圖 —— 這次是文件對、程式碼錯。

### 事實有兩個 owner (Duplicated ownership)

同一個事實在三處各寫一份：

| 事實                | 正牌 owner                              | 影子副本                                    |
| ------------------- | --------------------------------------- | ------------------------------------------- |
| agent session 路徑  | `svc/agent/providers/*.json` `sessionDirs` | `svc/stat` 的 viper 預設值（硬寫五條路徑）  |
| HTTP retry 語意     | —（無 owner）                            | `svc/fetch`、`svc/rule`、`svc/token` 各一份 |
| app 資料目錄        | gosdk `config.GetAppConfigDir()`        | `svc/stat/cache.go` 寫死 `~/.config/cc-plugin` |

`svc/session` 讀 provider config，`svc/stat` 不讀 —— 同一個 repo 裡對同一件事
有兩種做法時，先確認`哪一個是對的`，而不是假設較新的那個是對的。

三份 retry 都是 `maxAttempts = 5`、都是 200ms 指數退避，卻沒有任何一份
知道其他兩份存在。

### 檔名說謊 (Names that lie)

- `svc/plugin/discover.go` 裡面只有 `Catalog` 型別，沒有 discovery。
- `svc/plugin/discover_test.go` 裡面全是 `TestWalk_*` —— 測的是 `utils` 的函式。
- `model/readdesc.go` 的 `descMaxChars` 常數從未被使用，doc comment 卻聲稱
  會截斷。`寫得出 pass/fail 的斷言不放 Markdown` 同樣適用於 doc comment。
- `docs/superpowers/{plans,specs}/` 與 canonical 的 `plans/`、`docs/specs/`
  平行存在，同一種文件有兩個家。

## 處置 (Actions)

1. `utils/walk.go` → `svc/discover/discover.go`（連同其測試從 `svc/plugin` 移入）。
   `utils` 現在 internal import 為零。
2. `model/readdesc.go` → `utils/desc.go`。`model` 回歸純型別，不做 I/O。
3. `cmd/stats/` 子 package 併回 `package cmd`。
4. retry 先在 `utils/retry.go` 收斂，接著上游化成 `gosdk/http`（發版 `v1.3.1`），
   本 repo 刪掉本地副本改引用 `gohttp`。上游化時才發現`第四份重複`：
   429／5xx 的判定三處各寫一次，抽成 `gohttp.IsRetryableStatus` 後暴露出
   `svc/fetch` 原本把 429 當永久錯誤`、與另外兩處不一致` —— 統一後
   archive 下載遇到 rate limit 會退避重試而非直接失敗。
5. 新增 `svc/stat/source.go`：`sessionRoot(agentType, index, overrideKey)`
   從 `agent.Agents()` 取路徑，viper key 降級為`覆寫`而非`來源`。
6. `svc/plugin/manifest.go`（787 行）拆成 manifest／skillentry／scan；
   `svc/tui/tui.go`（1089 行）拆成 tui／update／view。
7. `CLAUDE.md` 新增分層表格，把 `cmd/ → svc/* → utils/`／`model/`
   寫成`不變式`而非慣例。

## 意外收穫：那個 commit 進來的 installs.json (Bonus finding)

`svc/update/data/installs.json` 一開始只被當成「誤 commit 的死檔」刪掉。
把 `svc/stat` 的快取也接到同一個 `config.GetAppConfigDir()` 之後才發現
`它是怎麼被生出來的`：

`config.Default(config.WithAppName("skills"))` 只在 `main.go` 呼叫。
`go test` 不經過 `main`，於是 `GetAppConfigDir()` 回傳`空字串`，
`filepath.Join("", "data", "installs.json")` 靜默變成`相對路徑`，
測試就把檔案寫進了 `svc/update/`。

更糟的是 `store_test.go` 本來就寫了 `t.Setenv("XDG_CONFIG_HOME", tmp)`
想要隔離 —— 那行`從來沒有生效過`。測試會過，是因為相對路徑剛好也不是
使用者的真實檔案。隔離手段失效與測試通過`同時成立`，所以沒人發現。

修法是 `utils.AppDataDir(appName)`：優先取 gosdk 的 config dir，
空字串時退回 XDG base（`XDG_CONFIG_HOME`，否則 `~/.config`）。
接上之後 `TestLoad_EmptyWhenMissing` 立刻紅燈 —— 它讀到了使用者的
真實 `installs.json`，證明隔離總算真的接上了。

教訓：`一個綠燈的測試不代表它測到了它宣稱要測的東西`。
測試裡的隔離設定（`t.Setenv`、fake、temp dir）本身也需要被驗證 ——
最快的方法是刻意讓它失敗一次，看看是不是真的失敗。

## 隔離稽核 (Isolation audit)

把上面的教訓套用到全 repo，逐一確認每個隔離手段`真的被程式碼讀到`：

| 隔離手段                            | 使用者                    | 稽核結果                                        |
| ----------------------------------- | ------------------------- | ----------------------------------------------- |
| `t.Setenv("XDG_CONFIG_HOME")`       | `svc/update`              | 修好前`從未生效`；接上 `AppDataDir` 後才真的隔離 |
| `t.Setenv("HOME")`                  | `svc/agent`、`svc/tui`    | `生效` —— 實測驗證（見下）                      |
| `t.Setenv("ANTHROPIC_BASE_URL")`    | `svc/token`               | `生效` —— 沒接上就會打真 API 而失敗，自證        |
| 直接注入 root/`discover` fake       | `svc/session`             | `生效` —— 不碰 env 與真實路徑                    |
| `t.TempDir()` 直接注入 `GlobalRulePath` | `cmd/install_test`     | `生效` —— 無 env 中介                            |

`t.Setenv("HOME")` 這項特別值得實測，因為 `go-homedir` 有 package-level
cache：第一次呼叫後改 `HOME` 會完全無效。寫了一支拋棄式 probe——先呼叫一次
`Agents()` 把 cache 灌熱、再改 `HOME`、再呼叫一次——確認第二次回傳的是
tempdir 路徑。`Agents()` 內的 `homedir.DisableCache = true` 確實有擋住這個坑。

留下的守衛是 `TestStorePathStaysAbsoluteAndIsolated` 與
`TestCacheFilePathStaysAbsoluteAndIsolated`：斷言`絕對路徑`且`落在 tempdir 內`。
兩個都用「把 bug 放回去」的方式驗證過會紅燈 ——
`沒有被看過失敗的斷言，不算斷言`。

## 帶走的教訓 (Takeaways)

- `一個資料夾叫 utils，不代表放進去的東西就是 util`。判準是 import 方向：
  leaf package import 了領域 package，就是放錯了。
- 拆檔案要先問「這個檔案的`責任`是什麼」，而不是「幾行了」。
  `tui.go` 拆成 state transition／render 兩份是有意義的；
  按行數對半切則不是。
- retry／backoff 這種`每個網路 package 都會長出來`的邏輯，
  第二次出現時就該抽走。等到第三次，三份已經各自漂移。
- `抽共用碼會逼出你原本沒發現的分歧`。三份 retry 迴圈長得夠像，
  所以合併很順；但合併後才看見 429 的處理其實三處不一致 ——
  分歧藏在「看起來一樣」的程式碼裡，只有真的擺在一起才現形。
- 上游化到 gosdk 時，`不要在 SDK 裡開第二個 owner`。
  `AppDataDir` 一度想搬進 `gosdk/utils`，但 `gosdk/config` 早就有
  `GetAppDataDir()`；正解是補 `config` 缺的 XDG 支援，而不是另起爐灶。
  順帶發現 `applyOptions` 自己也算了一次 config dir ——
  只改 `GetAppConfigDir()` 會讓 seed 與讀取落在不同目錄。
- 重構全程以 `go build` + `go vet` + `go test ./...` 逐步驗證，
  未 commit —— 設計／重構任務預設不自動 commit。
