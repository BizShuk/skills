# Session Event Semantic Colors Implementation Plan

> For agentic workers: REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax.

Goal: In the skills session detail view, color each timeline row according to its normalized event type.

Architecture: Keep model.SessionEvent, provider parsers, detail loading, and text formatting unchanged. Add one semantic SessionEvent-to-Lip Gloss color mapping in svc/tui/session.go. Build and truncate the plain row first, then apply foreground color so ANSI sequences cannot affect width or model data.

Tech Stack: Go 1.26.3, Bubble Tea 1.3.10, Lip Gloss 1.1.0, testify.

## Global Constraints

- tool_call uses orange 208.
- raw uses gray 244.
- event, system_event, or Role=system uses yellow 220.
- message with Role=user uses blue 39.
- message with Role=assistant uses green 42.
- Other combinations use fallback color 81.
- Role=system is checked after tool_call and raw but before message role colors.
- ANSI escape sequences exist only in TUI output, never in model.SessionEvent or service data.
- Do not add provider parser logic, model fields, or user-configurable palette options.
- Truncate the plain row before applying Lip Gloss.

---

### Task 1: Add failing semantic-color tests

Files:

- Modify: svc/tui/session_test.go — add Lip Gloss import and semantic color regression tests.

Interfaces:

- Produces the expected sessionEventAccent(model.SessionEvent) lipgloss.Color contract for Task 2.

- [ ] Step 1: Write the failing tests

Add the Lip Gloss import:

~~~
"github.com/charmbracelet/lipgloss"
~~~

Add these tests:

~~~
func TestSessionEventAccentUsesSemanticColors(t *testing.T) {
	cases := []struct {
		name  string
		event model.SessionEvent
		want  lipgloss.Color
	}{
		{name: "tool call", event: model.SessionEvent{Kind: "tool_call"}, want: lipgloss.Color("208")},
		{name: "raw", event: model.SessionEvent{Kind: "raw"}, want: lipgloss.Color("244")},
		{name: "event", event: model.SessionEvent{Kind: "event"}, want: lipgloss.Color("220")},
		{name: "system role", event: model.SessionEvent{Kind: "message", Role: "system"}, want: lipgloss.Color("220")},
		{name: "system event alias", event: model.SessionEvent{Kind: "system_event"}, want: lipgloss.Color("220")},
		{name: "user message", event: model.SessionEvent{Kind: "message", Role: "user"}, want: lipgloss.Color("39")},
		{name: "assistant message", event: model.SessionEvent{Kind: "message", Role: "assistant"}, want: lipgloss.Color("42")},
		{name: "unknown", event: model.SessionEvent{Kind: "future_event"}, want: lipgloss.Color("81")},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, sessionEventAccent(testCase.event))
		})
	}
}

func TestFormatSessionEventKeepsPlainContent(t *testing.T) {
	event := model.SessionEvent{
		Timestamp: time.Date(2026, time.July, 18, 9, 30, 0, 0, time.UTC),
		Role:      "tool",
		Kind:      "tool_call",
		Summary:   "apply_patch",
	}

	row := formatSessionEvent(event, 120)
	assert.Contains(t, row, "09:30:00")
	assert.Contains(t, row, "tool/tool_call")
	assert.Contains(t, row, "apply_patch")
}
~~~

- [ ] Step 2: Run the focused tests and verify the expected failure

Run:

~~~
gofmt -w svc/tui/session_test.go
go test ./svc/tui -run 'TestSessionEventAccentUsesSemanticColors|TestFormatSessionEventKeepsPlainContent' -count=1
~~~

Expected: compilation fails because sessionEventAccent is not defined yet. Do not modify production code before observing this failure.

- [ ] Step 3: Commit the red tests

~~~
git add svc/tui/session_test.go
git commit -m "test: define session event semantic colors"
~~~

### Task 2: Implement event row color mapping and rendering

Files:

- Modify: svc/tui/session.go around formatSessionEvent — add the fixed event palette, semantic mapping helper, and final row styling.
- Test: svc/tui/session_test.go — pass the tests from Task 1.

Interfaces:

- Consumes: model.SessionEvent, strings, existing truncateRune, and Lip Gloss.
- Produces: sessionEventAccent(event model.SessionEvent) lipgloss.Color.

- [ ] Step 1: Add the fixed palette and precedence-aware helper

Add this palette near the existing session color constants:

~~~
var sessionEventColors = map[string]lipgloss.Color{
	"tool_call":         lipgloss.Color("208"),
	"raw":               lipgloss.Color("244"),
	"system_event":      lipgloss.Color("220"),
	"user_message":      lipgloss.Color("39"),
	"assistant_message": lipgloss.Color("42"),
}

const sessionEventFallbackColor = lipgloss.Color("81")
~~~

Add this helper before formatSessionEvent:

~~~
func sessionEventAccent(event model.SessionEvent) lipgloss.Color {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	role := strings.ToLower(strings.TrimSpace(event.Role))
	switch {
	case kind == "tool_call":
		return sessionEventColors["tool_call"]
	case kind == "raw":
		return sessionEventColors["raw"]
	case kind == "event", kind == "system_event", role == "system":
		return sessionEventColors["system_event"]
	case kind == "message" && role == "user":
		return sessionEventColors["user_message"]
	case kind == "message" && role == "assistant":
		return sessionEventColors["assistant_message"]
	default:
		return sessionEventFallbackColor
	}
}
~~~

- [ ] Step 2: Style only after plain row truncation

Keep the current timestamp, label, summary, sanitization, and width logic in formatSessionEvent. Replace only its final return with:

~~~
line := truncateRune(fmt.Sprintf("%s  %-18s %s", timestamp, label, summary), maxWidth)
return lipgloss.NewStyle().Foreground(sessionEventAccent(event)).Render(line)
~~~

The full line, including timestamp, label, and summary, is colored; no styling is applied to event itself.

- [ ] Step 3: Run focused tests and the TUI regression suite

~~~
gofmt -w svc/tui/session.go svc/tui/session_test.go
go test ./svc/tui -run 'TestSessionEventAccent|TestFormatSessionEventKeepsPlainContent' -count=1
go test ./svc/tui -count=1
~~~

Expected: all focused tests and existing TUI tests pass.

- [ ] Step 4: Commit the implementation

~~~
git add svc/tui/session.go svc/tui/session_test.go
git commit -m "feat: color session events by type"
~~~

### Task 3: Complete repository verification

Files:

- No additional source files. Preserve the pre-existing uncommitted change in docs/superpowers/specs/2026-07-18-session-tui-design.md.

Interfaces:

- Consumes: Task 2 implementation and tests.
- Produces: verified event color behavior with no parser or model regression.

- [ ] Step 1: Run the complete test suite

~~~
go test ./... -count=1
~~~

Expected: all packages pass.

- [ ] Step 2: Run static analysis and build

~~~
go vet ./...
go build -o /tmp/skills-session .
~~~

Expected: both commands exit zero.

- [ ] Step 3: Check diff and working tree scope

~~~
git diff --check
git status --short
git log --oneline -4
~~~

Expected: no whitespace errors; only the known pre-existing spec edit remains uncommitted after the feature commits.
