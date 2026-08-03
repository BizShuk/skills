# Plugin Skills Directory 解析設計

## 問題

`plugin.json` 可將 `skills` 欄位設為相對資料夾，例如：

```json
{
  "skills": ["./skills"]
}
```

目前有兩個串聯問題：marketplace local plugin 不會讀取其子目錄內的
`plugin.json`；即使直接掃描該 plugin，`scanSkills` 也會將每個值視為
`SKILL.md` 檔案路徑，再取 parent directory。因此 `./skills` 會錯誤解析為
plugin root，漏掉實際存在的 `./skills/SKILL.md`，使 TUI 只顯示無法選取的
空分類。

## 目標行為

1. marketplace local plugin 應合併其 marketplace entry 與子目錄
   `plugin.json` 宣告的 `skills` paths。
2. 對每個合法且位於既有 scan boundary 內的 `skills` path：

   - path 是直接包含 `SKILL.md` 的資料夾時，將該資料夾加入為單一 skill。
   - path 是包含多個 `<name>/SKILL.md` 子目錄的資料夾時，加入所有直接子 skill。
   - path 直接指向 `SKILL.md` 時，保留既有相容行為。
   - path 不存在、格式不支援或越過 scan boundary 時，維持目前的忽略行為。
   - 同一路徑同時由慣例掃描與 manifest 發現時，只保留一份。

## 方案比較

- 推薦：依實際檔案類型解析 manifest path。可涵蓋單一 skill directory、skill
  collection directory 與既有 file path，且不綁死 `./skills` 名稱。
- 僅特殊處理 `./skills`：修改較小，但其他合法自訂目錄仍會失敗。
- 要求上游改成 `./skills/SKILL.md`：不修復本工具與既有 marketplace 的相容性。

採用第一種方案。

## 實作邊界

- 修改 `svc/plugin/manifest.go`，讓 marketplace local plugin 合併子
  `plugin.json` 的 `skills` paths，並修正 additive skill path 解析。
- 在 `svc/plugin/manifest_test.go` 新增完整 marketplace blender-toolkit fixture
  與 collection directory 的回歸測試。
- 不修改 TUI、安裝流程、remote fetch 或 GitHub subpath 行為。

## 驗證

- 先確認新增測試在現有程式上失敗。
- 實作最小修復後執行 `go test ./svc/plugin/...`。
- 執行 `go test ./...` 與 `go build -o bin/skills .`。
- 以本機 blender-toolkit fixture 確認 catalog 顯示一個可選 skill。
