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
| 啟動 `skills add <path>` 後進入 TUI | 看到 catalog 中每個 plugin 的 header，所有 skills 與 subagents 都隱藏 |
| `↓` / `↑` | 在 headers 之間移動（nested children 的 headers 即使父層 fold 仍會渲染——`rebuildVisible` 不論 fold 與否都會走訪子節點並把子 header 加入 `out`，使用者透過「header 底下是否有 skill / subagent row」判斷 fold 狀態） |
| `→` 在 header | cascade 展開整個 subtree（該 header 及其子孫的 skills/subagents 全變可見） |
| `←` 在 header | cascade 收合整個 subtree |
| 空白鍵在 header | 不受 fold 狀態影響，仍能「全選 / 取消全選」subtree 內所有 skills 與 subagents |
| 鍵入搜尋文字 | `rebuildVisible` 命中 header 名稱時仍會列出 header；命中 skill description 或 subagent description 時，header 因為 `skillDirectMatch` / `subagentDirectMatch` 也會顯示，但底下的 skill row 因 fold 仍藏起來；行為與目前一致 |

由於 header 本身沒有視覺 fold 指示（既有的 bubble-tea 渲染維持原樣），使用者得透過「看 header 底下是否有 skill/subagent row」判斷 fold 狀態。本 spec 不引入新圖示（YAGNI）。

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
