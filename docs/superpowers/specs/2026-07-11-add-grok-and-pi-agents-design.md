# 新增 grok 與 pi 代理器設計 (Design Spec)

日期：2026-07-11
目標：在 `skills` 項目中新增 `grok` 與 `pi` 代理器 (agent) 支援。

## 變更範圍 (Scope)

### 1. 新增代理器配置檔案 (Add JSON Files)

在 `svc/agent/providers/` 目錄下新增兩個 JSON 設定檔，分別代表 `grok` 與 `pi` 代理器的安裝與偵測路徑。

`svc/agent/providers/grok.json`
```json
{
  "type": "grok",
  "displayName": "Grok",
  "projectSkillsDir": ".grok/skills",
  "userSkillsDir": "~/.grok/skills",
  "projectAgentsDir": ".grok/agents",
  "userAgentsDir": "~/.grok/agents",
  "detectDir": "~/.grok"
}
```

`svc/agent/providers/pi.json`
```json
{
  "type": "pi",
  "displayName": "Pi",
  "projectSkillsDir": ".pi/skills",
  "userSkillsDir": "~/.pi/skills",
  "projectAgentsDir": ".pi/agents",
  "userAgentsDir": "~/.pi/agents",
  "detectDir": "~/.pi"
}
```

### 2. 更新測試程式 (Update Test Suite)

修改 `svc/agent/agent_test.go`：
- 修改 `TestLoadAllReturnsSixProviders` 為 `TestLoadAllReturnsEightProviders`，將預期代理器長度從 `6` 改為 `8`。
- 在 `want` 清單中補上 `grok` 與 `pi`。

### 3. 建立專案結構規範檔案 (Create Standard Workspace Files)

依據專案工作區的方向性規範，補齊缺失的專案規範檔案：
- 建立 `CLAUDE.md` (技術脈絡)。
- 建立 `AGENTS.md` (軟連結指向 `CLAUDE.md`)。

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)

執行 Go 測試以確認新增的 JSON 設定能被正確載入、解析，並通過欄位驗證：
`go test ./svc/agent/...`
