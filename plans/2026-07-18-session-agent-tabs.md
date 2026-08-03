# Session Agent Tabs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 將 skills session 的 list view 改為 agent tabs，支援左右鍵循環切換、每個 agent 保存自己的 cursor/offset、date-first session rows，以及一致的彩色 tab render。

**Architecture:** 在既有 SessionModel 內將輸入 sessions 按 agent 做一次分組，保留目前 active tab 的 items 供既有 list/detail 流程使用。新增 sessionTab、active agent index、每 agent 的 cursor/offset maps；tab 切換只改變記憶體 state，不觸發 detail loading 或 filesystem I/O。Lip Gloss 負責固定 agent accent palette、active/inactive tab styles 與 cursor marker，session data 本身不保存 ANSI。

**Tech Stack:** Go 1.26.3、Bubble Tea 1.3.10、Lip Gloss 1.1.0、既有 svc/tui/session.go、testify。

## Global Constraints

- ←/→ 在 session list view 只負責切換 agent tab；Enter 與滑鼠左鍵開啟 detail。
- 每個 agent 保存獨立的 cursor 與 list offset；切換 tab 不遺失位置。
- list row 固定為 YYYY-MM-DD HH:MM:SS  SESSION_ID，日期/時間在 session ID 前。
- 只有有 sessions 的 agent 才建立 tab；tab 順序沿用輸入 sessions 的 first-seen order。
- active tab 使用 agent accent color、粗體與反白；inactive tab 使用該 accent color 的 dim style。
- 不新增第三方 dependency；沿用既有 Bubble Tea/Lip Gloss。
- ANSI escape sequence 只存在於 TUI rendering，不進入 model 或 service data。
- 空 session list 維持既有 plain-text output，不啟動空 TUI。
- provider discovery、detail parser、session.LoadDetail 與 lazy loading contract 不變。
- 每個 production behavior 先寫 focused failing test，再寫最小 production code；每個 task 完成執行 focused test。

---

## File Map

- Modify: svc/tui/session.go — agent grouping state、tab navigation、row/date rendering、Lip Gloss styles。
- Modify: svc/tui/session_test.go — tab grouping、navigation、position restore、render regression tests。
- Modify: README.md — 更新 skills session 的 list controls 與 date-first layout 說明。

不修改 model、svc/session、cmd/session.go；這次變更只屬於 TUI presentation/state。

---

### Task 1: 建立 agent tab grouping 與 active list state

Files:

- Modify: svc/tui/session.go
- Modify: svc/tui/session_test.go

Interfaces:

~~~go
type sessionTab struct {
    agent string
    items []model.AgentSession
}

func buildSessionTabs(items []model.AgentSession) []sessionTab
func (m SessionModel) activeAgentName() string
func (m SessionModel) activeItems() []model.AgentSession
~~~

SessionModel 保留既有 items、cursor、offset 作為目前 active tab 的 working set，並新增 tabs、activeAgent、cursorByAgent、offsetByAgent。

- [ ] Step 1: Write the failing grouping test

~~~go
func TestSessionModelGroupsSessionsIntoFirstSeenAgentTabs(t *testing.T) {
    items := []model.AgentSession{
        {Agent: "codex", ID: "codex-1"},
        {Agent: "claude-code", ID: "claude-1"},
        {Agent: "codex", ID: "codex-2"},
    }
    m := NewSessionModel(items, nil)

    require.Equal(t, []string{"codex", "claude-code"}, m.agentNames())
    require.Equal(t, 0, m.activeAgent)
    require.Equal(t, "codex", m.activeAgentName())
    assert.Equal(t, []string{"codex-1", "codex-2"}, sessionIDs(m.activeItems()))
    assert.Equal(t, 0, m.cursorByAgent["codex"])
    assert.Equal(t, 0, m.cursorByAgent["claude-code"])
}
~~~

Add sessionIDs helper that returns the ID of each item in order.

- [ ] Step 2: Run the focused test to verify it fails

~~~bash
go test ./svc/tui -run TestSessionModelGroupsSessionsIntoFirstSeenAgentTabs -count=1
~~~

Expected: FAIL because agentNames, tabs, and per-agent state do not exist.

- [ ] Step 3: Implement minimal grouping state

Add sessionTab, tabs, activeAgent, cursorByAgent, and offsetByAgent. Implement first-seen grouping without changing input order inside each agent:

~~~go
func buildSessionTabs(items []model.AgentSession) []sessionTab {
    byAgent := make(map[string]int)
    tabs := make([]sessionTab, 0)
    for _, item := range items {
        index, ok := byAgent[item.Agent]
        if !ok {
            index = len(tabs)
            byAgent[item.Agent] = index
            tabs = append(tabs, sessionTab{agent: item.Agent})
        }
        tabs[index].items = append(tabs[index].items, item)
    }
    return tabs
}
~~~

Update NewSessionModel to build tabs, initialize both maps for every tab, and set items to the first tab items. For empty input, leave items empty and activeAgent at zero. Add agentNames, activeAgentName, and activeItems as unexported helpers.

- [ ] Step 4: Run focused tests

~~~bash
gofmt -w svc/tui/session.go svc/tui/session_test.go
go test ./svc/tui -run 'TestSessionModelGroupsSessionsIntoFirstSeenAgentTabs|TestSessionModelStartsOnListAndRendersRows|TestRunSession' -count=1
~~~

Expected: all selected tests pass.

- [ ] Step 5: Commit

~~~bash
git add svc/tui/session.go svc/tui/session_test.go
git commit -m "feat: group session list by agent"
~~~

### Task 2: Add left/right tab navigation and per-agent position restore

Files:

- Modify: svc/tui/session.go
- Modify: svc/tui/session_test.go

Interfaces:

~~~go
func (m *SessionModel) switchAgent(delta int)
func (m *SessionModel) saveActivePosition()
func (m *SessionModel) loadActivePosition()
~~~

switchAgent wraps modulo len(m.tabs), updates m.items to the target tab, restores cursorByAgent and offsetByAgent, then calls ensureListCursorVisible.

- [ ] Step 1: Write failing navigation and restore tests

~~~go
func TestSessionModelLeftRightSwitchesTabsAndRestoresCursor(t *testing.T) {
    items := []model.AgentSession{
        {Agent: "codex", ID: "codex-1"},
        {Agent: "codex", ID: "codex-2"},
        {Agent: "claude-code", ID: "claude-1"},
    }
    m := NewSessionModel(items, nil)

    updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
    m = mustSessionModel(t, updated)
    require.Equal(t, "codex-2", m.selectedItem().ID)

    updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
    m = mustSessionModel(t, updated)
    require.Equal(t, "claude-code", m.activeAgentName())
    require.Equal(t, "claude-1", m.selectedItem().ID)

    updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
    m = mustSessionModel(t, updated)
    assert.Equal(t, "codex", m.activeAgentName())
    assert.Equal(t, "codex-2", m.selectedItem().ID)
}

func TestSessionModelTabNavigationWraps(t *testing.T) {
    m := NewSessionModel([]model.AgentSession{
        {Agent: "codex", ID: "codex-1"},
        {Agent: "claude-code", ID: "claude-1"},
    }, nil)

    updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
    m = mustSessionModel(t, updated)
    assert.Equal(t, "claude-code", m.activeAgentName())

    updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
    m = mustSessionModel(t, updated)
    assert.Equal(t, "codex", m.activeAgentName())
}
~~~

- [ ] Step 2: Run navigation tests to verify failure

~~~bash
go test ./svc/tui -run 'TestSessionModelLeftRightSwitchesTabsAndRestoresCursor|TestSessionModelTabNavigationWraps' -count=1
~~~

Expected: FAIL because list KeyRight currently opens detail and KeyLeft has no list behavior.

- [ ] Step 3: Implement position save/restore and key semantics

~~~go
func (m *SessionModel) switchAgent(delta int) {
    if len(m.tabs) <= 1 {
        return
    }
    m.saveActivePosition()
    m.activeAgent = (m.activeAgent + delta + len(m.tabs)) % len(m.tabs)
    m.items = append(m.items[:0], m.tabs[m.activeAgent].items...)
    m.loadActivePosition()
    m.ensureListCursorVisible()
}
~~~

In updateKey for sessionListPhase, make KeyLeft call switchAgent(-1), KeyRight call switchAgent(1), and KeyEnter call openSelected. Keep detail left/Esc behavior unchanged. Keep mouse click opening the active-tab row. For a single tab, left/right must be no-op and must not invoke the loader.

- [ ] Step 4: Update the existing right-arrow detail test to Enter

Rename TestSessionModelRightArrowLoadsDetail to TestSessionModelEnterLoadsDetail and send a tea.KeyMsg with Type tea.KeyEnter. Keep assertions for Loading and the injected loader. Add an assertion that right arrow in a multi-agent list changes activeAgent without returning a loader command.

- [ ] Step 5: Run focused navigation and regression tests

~~~bash
gofmt -w svc/tui/session.go svc/tui/session_test.go
go test ./svc/tui -run 'TestSessionModel|TestRunSession' -count=1
~~~

Expected: all session model tests pass, including mouse click, Enter detail, Esc/Left detail return, and scroll bounds.

- [ ] Step 6: Commit navigation

~~~bash
git add svc/tui/session.go svc/tui/session_test.go
git commit -m "feat: switch session tabs with arrow keys"
~~~

### Task 3: Render colored tabs and date-first rows

Files:

- Modify: svc/tui/session.go
- Modify: svc/tui/session_test.go

Interfaces:

~~~go
func sessionAgentAccent(agent string) lipgloss.Color
func renderSessionTab(agent string, count int, active bool) string
func formatSessionRow(item model.AgentSession) string
~~~

Use these fixed accents:

~~~go
var sessionAgentColors = map[string]lipgloss.Color{
    "claude-code": lipgloss.Color("135"),
    "codex": lipgloss.Color("39"),
    "grok": lipgloss.Color("214"),
    "antigravity": lipgloss.Color("42"),
    "antigravity-cli": lipgloss.Color("37"),
    "hermes-agent": lipgloss.Color("33"),
    "opencode": lipgloss.Color("205"),
    "pi": lipgloss.Color("141"),
}

const sessionFallbackColor = lipgloss.Color("81")
~~~

- [ ] Step 1: Write failing render tests

~~~go
func TestSessionModelRendersAgentTabsAndDateFirstRows(t *testing.T) {
    last := time.Date(2026, 7, 18, 2, 58, 27, 0, time.UTC)
    m := NewSessionModel([]model.AgentSession{
        {Agent: "codex", ID: "codex-1", LastActivity: last},
        {Agent: "claude-code", ID: "claude-1", LastActivity: last.Add(-time.Hour)},
    }, nil)

    view := m.View()
    codexTab := strings.Index(view, "codex 1")
    claudeTab := strings.Index(view, "claude-code 1")
    date := strings.Index(view, "2026-07-18 02:58:27")
    id := strings.Index(view, "codex-1")

    require.GreaterOrEqual(t, codexTab, 0)
    require.GreaterOrEqual(t, claudeTab, 0)
    require.GreaterOrEqual(t, date, 0)
    require.GreaterOrEqual(t, id, 0)
    assert.Less(t, date, id)
}

func TestSessionAgentAccentUsesKnownAndFallbackColors(t *testing.T) {
    assert.Equal(t, lipgloss.Color("39"), sessionAgentAccent("codex"))
    assert.Equal(t, sessionFallbackColor, sessionAgentAccent("future-agent"))
    assert.Contains(t, renderSessionTab("codex", 2, true), "codex 2")
    assert.Contains(t, renderSessionTab("codex", 2, false), "codex 2")
}
~~~

Add strings, time, and lipgloss to test imports.

- [ ] Step 2: Run render tests to verify failure

~~~bash
go test ./svc/tui -run 'TestSessionModelRendersAgentTabsAndDateFirstRows|TestSessionAgentAccentUsesKnownAndFallbackColors' -count=1
~~~

Expected: FAIL because current list has no tab row, row format starts with agent name, and no accent helpers exist.

- [ ] Step 3: Implement palette and tab rendering

Import lipgloss. Implement sessionAgentAccent with map lookup and sessionFallbackColor fallback. Implement tab styles:

~~~go
func renderSessionTab(agent string, count int, active bool) string {
    accent := sessionAgentAccent(agent)
    style := lipgloss.NewStyle().Padding(0, 1)
    if active {
        style = style.Foreground(lipgloss.Color("15")).Background(accent).Bold(true)
    } else {
        style = style.Foreground(accent).Faint(true)
    }
    return style.Render(fmt.Sprintf("%s %d", agent, count))
}
~~~

Add viewAgentTabs that renders m.tabs in order, joining each rendered tab with one space. Insert the tab row before the controls and use one shared sessionListFirstRow coordinate for rendering and mouse mapping. The list header should be:

~~~go
b.WriteString("Session list\n\n")
b.WriteString(m.viewAgentTabs())
b.WriteByte('\n')
b.WriteString("←/→ switch agent, ↑↓ move session, enter/click open, esc quit\n\n")
~~~

Update formatSessionRow to omit the agent and put last activity first:

~~~go
func formatSessionRow(item model.AgentSession) string {
    lastActivity := "-"
    if !item.LastActivity.IsZero() {
        lastActivity = item.LastActivity.Local().Format("2006-01-02 15:04:05")
    }
    return truncateRune(fmt.Sprintf("%s  %s", lastActivity, item.ID), 120)
}
~~~

Use the active agent accent for the > cursor marker only; keep date, ID and summaries plain/high-contrast. Ensure WindowSizeMsg subtracts the new header height and updateMouse uses the same sessionListFirstRow constant.

- [ ] Step 4: Run render and all TUI tests

~~~bash
gofmt -w svc/tui/session.go svc/tui/session_test.go
go test ./svc/tui -run 'TestSessionModel|TestRunSession|TestSessionAgentAccent' -count=1
~~~

Expected: all tests pass, including tab labels, date-first row ordering, mouse row mapping and color helper behavior.

- [ ] Step 5: Commit render change

~~~bash
git add svc/tui/session.go svc/tui/session_test.go
git commit -m "feat: color session agent tabs"
~~~

### Task 4: Synchronize user documentation and complete verification

Files:

- Modify: README.md:39-50

- [ ] Step 1: Write the documentation update

Update the skills session section to state the final controls and row format:

~~~markdown
有 session 時會進入互動式 TUI。列表上方會以 tab 分組顯示有 session 的 agents：
使用 ←/→ 切換 agent，↑/↓ 移動該 agent 的 session，按 Enter 或滑鼠左鍵
開啟 detail；detail 畫面按 ← 或 Esc 返回列表。列表 row 會先顯示
YYYY-MM-DD HH:MM:SS，再顯示 session ID；每個 agent 會保留自己的選取位置。
完整 transcript 採 lazy loading，只在開啟選取的 session 時讀取。
~~~

- [ ] Step 2: Run focused command/TUI tests

~~~bash
go test ./cmd ./svc/tui -count=1
~~~

Expected: command registration, empty plain output, tab navigation, detail loading and render tests pass.

- [ ] Step 3: Run complete verification

~~~bash
go test ./... -count=1
go vet ./...
go build -o /tmp/skills-session .
git diff --check
git status --short
~~~

Expected: all tests pass, vet/build exit zero, no whitespace errors, and only intended TUI/README files are changed. Run the existing empty-result smoke test with a temporary HOME and a temporary working directory; then launch /tmp/skills-session session under a PTY and confirm the first screen contains Session list. The PTY runner may buffer alternate-screen frames, so state-transition tests are authoritative evidence for arrow/tab/detail transitions.

- [ ] Step 4: Commit documentation and verification unit

~~~bash
git add README.md
git commit -m "docs: document session agent tabs"
~~~

