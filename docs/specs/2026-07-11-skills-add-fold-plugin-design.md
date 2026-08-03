# 2026-07-11 skills add — fold all plugins by default

## 目的 (Purpose)

`services/tui` 的 `NewModel` 在初始化 fold map 時只把 remote root（`OwnerRepo != ""`）預設為 fold，local root 反而預設展開。這讓初次打開 TUI 時的畫面不一致：local plugin 的 skills 立即可見、remote plugin 的則隱藏。本 spec 把 root 層級的行為統一為「永遠 fold」，與既有 nested children 的預設 fold 對齊。

## 設計 (Design)

唯一修改在 `svc/tui/tui.go::NewModel`（line 152–164）初始化 `folded` map 的那段：

```go
// 修改前
for _, root := range cat.Roots {
    if root.OwnerRepo != "" {
        m.folded[root] = true
    }
    foldNested(root)
}

// 修改後
for _, root := range cat.Roots {
    m.folded[root] = true   // 所有 root 一律 fold；nested 由 foldNested 接手
    foldNested(root)
}
```

`foldNested` 遞迴把所有 nested children 也設成 fold，行為不變。`Update` 既有的 cascade fold/unfold 路徑（`foldSubtree`/`unfoldSubtree`，line 1026–1046）、`View` 的 header 與 skill/subagent row 渲染邏輯、`Selection()` 與 `Run()` API 全部不動。

## 互動 (Interaction)

| 操作 | 行為 |
| --- | --- |
| 啟動 `skills add <path>` 後進入 TUI | 看到 catalog 中每個 root plugin 的 header，所有 nested children（含其 headers）與底下所有 skills / subagents 都隱藏 |
| `↓` / `↑` | 只在 root headers 之間移動——nested children 的 header 在父層 fold 時不渲染 |
| `→` 在 header | cascade 展開整個 subtree（該 header 及其子孫的 headers、skills、subagents 全變可見） |
| `←` 在 header | cascade 收合整個 subtree |
| 空白鍵在 header | 不受 fold 狀態影響，仍能「全選 / 取消全選」subtree 內所有 skills 與 subagents |
| 鍵入搜尋文字 | `rebuildVisible` 命中 header 名稱時仍會列出 header；命中 skill description 或 subagent description 時，header 因為 `skillDirectMatch` / `subagentDirectMatch` 也會顯示，底下的 skill row 因 fold 仍藏起來；行為與 fold-everything 對齊 |

## 修正 (Amendment, 2026-07-11)

實作 `NewModel` fold-all 後回歸一輪使用者回報：remote root plugin（例如 `voltagent/awesome-claude-code-subagents`，其 marketplace.json 宣告十多個 sub-plugins）在 TUI 初始畫面仍把所有 child headers 顯示出來，視覺上看不出 fold 與否。檢查 `rebuildVisible` 發現 child walk 不論 fold 狀態都會遞迴走訪子節點——`m.folded[c]` 只 gate 該節點自己的 skill/subagent row 是否渲染，child header 仍照走訪結果加入 `out`。

修正：child walk 在 `q == "" && m.folded[c]` 時跳過（搜尋時仍必須下鑽才能找到命中）。配套調整：

- `TestNewModelFoldsNestedSubPluginsByDefault` → `TestNewModelHidesNestedChildrenWhenParentFolded`：斷言翻成「`inner` 也不可見」。
- `TestRightArrowExpandsAndLeftFolds` 流程更新：初始 1 row（outer only）→ Right cascade unfold 至 3 rows → Left cascade fold 回 1 row。
- `TestLayer2_FoldToggle` 流程更新：先 Right on cc-plugin 取得 layer-2 可達性，再 Down + Right on layer-2 測試 selective fold（cc-plugin 仍展開、layer-2 subtree 收合）。
- 新增 `TestRemoteMarketplaceWithManyChildren_AllHiddenByDefault`：直接復現使用者截圖的 catalog 形狀（10 children 的 remote root），初始只見 10 個 root headers、所有 nested children 與 python-pro 都不見；Right on remote root 後 cascade 展開到底。

由於 header 本身沒有視覺 fold 指示（既有的 bubble-tea 渲染維持原樣），使用者得透過「看 header 底下是否有 child header / skill / subagent row」判斷 fold 狀態。本 spec 不引入新圖示（YAGNI）。

## 資料流 (Data Flow)

`Selection()` 完全只看 `checked`/`checkedSubagent` map 與 `agents`/`global`，fold 狀態不在輸出路徑上。pipeline（`install.Apply`）收到的只是哪些 skill/subagent paths 被勾選，根本不知道 fold 與否。

## 錯誤處理 (Error Handling)

無 — fold 純屬 view 邏輯，不參與 install pipeline。即使使用者完全沒有展開任何 plugin 也可以：

- 在任意 header 上按空白鍵批次勾選底下全部
- 不展開直接 `Enter` 進到 agent-phase、跳 level-phase 後送出空 selection（pipeline 拿到空 selection 視為「不安裝」，與目前行為一致）

## 測試 (Testing)

### 修改既有測試

`TestRemoteRootPluginsAreFoldedByDefault`（`svc/tui/tui_test.go` line 635–657）——名稱與 docstring 需要翻新成「所有 root 預設 fold」：

- 斷言翻轉：`assert.NotContains(t, view, "local-skill", ...)` 與 `assert.NotContains(t, view, "remote-skill", ...)`
- 新增正向斷言：先把 cursor 移到 local root header、按 `→`，驗證 `local-skill` 出現；同樣對 remote root 做一次

### 新增測試

`TestAllRootsFolded_BothRequireExpansion` 對應正向與反向行為：

- 用同時含有 local 與 remote root 的 fixture
- 初始 `View()` 不含任一 skill
- 個別按 `→` 展開任一 root 才會看到對應 skill
- 同時按 `→` 展開兩個 root，兩個 skill 都可見

依既有 `TestCascadeUnfold_ParentShowsAllDescendants` 的 fixture 模式新增 `TestCascadeUnfoldOnLocalRoot` 確保 local root 走的是同一個 cascade unfold 路徑（避免對 local root 走不同的 fold key 而漏掉 cascade）。

### 既有測試無須更動

- `TestRightArrowExpandsAndLeftFolds`（nested 行為）
- `TestCascadeUnfold_ParentShowsAllDescendants`
- `TestSkillDescriptionFoldUnfold`（skill 層級 fold/unfold，與 header fold 無關）
- `TestRemoteRootPluginsAreFoldedByDefault` 翻新後繼續守住契約

## 範疇外 (Out of Scope)

- 不加 header fold 視覺指示（如 `▶`/`▼` 圖示或縮排變化）
- 不加 `Ctrl+E` 全展開 / `Ctrl+W` 全收合 快捷鍵
- 不動 `Run()` 簽名、不動 `Selection()`、不動 `install.Apply`、不動 `cmd/root.go`

## 風險 (Risks)

- 既有使用情境「local plugin 直接展開讓人看見 skill」會消失。使用者要按 `→` 才能進去看。可接受的 UX 取捨：consistency over convenience，且 cascade 全選仍能在不展開的情況下勾選。
- 既有測試若未覆蓋到 local root 預設 expand 行為（grep 後沒有），零迴歸風險。
