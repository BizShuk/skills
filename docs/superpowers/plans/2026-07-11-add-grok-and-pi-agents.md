# 新增 grok 與 pi 代理器實作計畫 (Grok and Pi Agent Implementation Plan)

> `For agentic workers:` REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

`Goal:` 在 `skills` 項目中新增 `grok` 與 `pi` 代理器支援，包含 JSON 配置檔案、測試更新與專案結構規範檔案。

`Architecture:` 本次變更為純配置驅動，透過新增 JSON 檔案定義兩個代理器，並更新對應的單元測試。同時於 repo 根目錄補齊專案的方向性規範檔案以滿足全工作區管理規範。

`Tech Stack:` Go, Go Embed, Go Test

## 全域約束 (Global Constraints)

- `繁體中文為主，術語以 local language 搭配英文圓括號。`
- `不使用粗體，一律以 backtick 強調。`
- `目標路徑為獨立目錄：.grok/skills 與 .pi/skills。`

---

### Task 1: 新增代理器設定 JSON 檔案

`Files:`
- Create: `svc/agent/providers/grok.json`
- Create: `svc/agent/providers/pi.json`

`Interfaces:`
- Consumes: 無
- Produces: 供 `svc/agent/agent.go` 之 `go:embed` 載入的 JSON 檔案

- [ ] `Step 1:` 建立並寫入 `svc/agent/providers/grok.json`

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

- [ ] `Step 2:` 建立並寫入 `svc/agent/providers/pi.json`

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

- [ ] `Step 3:` 提交變更

```bash
git add svc/agent/providers/grok.json svc/agent/providers/pi.json
git commit -m "feat: add grok and pi provider configuration files"
```

---

### Task 2: 更新代理器單元測試

`Files:`
- Modify: `svc/agent/agent_test.go`

`Interfaces:`
- Consumes: `svc/agent/providers/grok.json`, `svc/agent/providers/pi.json`
- Produces: 綠燈的測試套件

- [ ] `Step 1:` 修改 `svc/agent/agent_test.go` 中 `TestLoadAllReturnsSixProviders`

將第 14 至 26 行的測試修改為：

```go
func TestLoadAllReturnsEightProviders(t *testing.T) {
	all := LoadAll()
	assert.Len(t, all, 8)
	want := []string{
		"claude-code", "antigravity", "antigravity-cli",
		"codex", "opencode", "hermes-agent", "grok", "pi",
	}
	got := make([]string, 0, len(all))
	for _, p := range all {
		got = append(got, string(p.Type))
	}
	assert.ElementsMatch(t, want, got)
}
```

- [ ] `Step 2:` 執行單元測試以驗證變更

Run: `go test ./svc/agent/... -v`
Expected: PASS

- [ ] `Step 3:` 提交變更

```bash
git add svc/agent/agent_test.go
git commit -m "test: update agent test to assert 8 providers including grok and pi"
```

---

### Task 3: 補齊專案結構規範檔案

`Files:`
- Create: `CLAUDE.md`
- Create: `AGENTS.md`

`Interfaces:`
- Consumes: 全工作區 `~/projects/` 規範
- Produces: `CLAUDE.md` 技術脈絡與其軟連結 `AGENTS.md`

- [ ] `Step 1:` 建立並寫入 `CLAUDE.md`

```markdown
# CLAUDE.md

## 專案結構 (Project Structure)

本專案是以 Go 重寫的 `skills` 輔助工具。主要架構如下：
- `cmd/skills/main.go` - CLI 入口點。
- `svc/agent/` - 代理器設定與安裝路徑管理。
- `svc/agent/providers/` - 代理器配置 JSON 檔案。

## 常用指令 (Commands)

- 測試：`go test ./...`
- 編譯：`go build -o bin/skills ./cmd/skills`
```

- [ ] `Step 2:` 建立軟連結 `AGENTS.md` 指向 `CLAUDE.md`

Run: `ln -sf CLAUDE.md AGENTS.md`
Expected: 成功建立軟連結

- [ ] `Step 3:` 提交變更

```bash
git add CLAUDE.md AGENTS.md
git commit -m "docs: add CLAUDE.md and AGENTS.md symlink"
```
