package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func createCodexIndexFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		cwd TEXT NOT NULL,
		created_at_ms INTEGER,
		updated_at_ms INTEGER
	)`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestDiscoverCodexUsesThreadIndexWithoutReadingRollouts(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(root, "state_5.sqlite")
	db := createCodexIndexFixture(t, indexPath)
	cwd := filepath.Join(root, "workspace")
	otherCWD := filepath.Join(root, "other")
	activePath := filepath.Join(root, "active.jsonl")
	archivedPath := filepath.Join(root, "archived.jsonl")
	require.NoError(t, os.WriteFile(activePath, []byte("not-json"), 0o644))
	require.NoError(t, os.WriteFile(archivedPath, []byte("not-json"), 0o644))

	_, err := db.Exec(`INSERT INTO threads
		(id, rollout_path, created_at, updated_at, cwd, created_at_ms, updated_at_ms)
		VALUES
		('active', ?, 10, 20, ?, 10001, 20001),
		('archived', ?, 30, 40, ?, NULL, NULL),
		('other', '/tmp/other.jsonl', 50, 60, ?, 50001, 60001)`,
		activePath, cwd, archivedPath, cwd, otherCWD,
	)
	require.NoError(t, err)

	got, err := discoverCodex(indexPath, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	byID := map[string]model.AgentSession{got[0].ID: got[0], got[1].ID: got[1]}
	assert.Equal(t, activePath, byID["active"].Path)
	assert.Equal(t, time.UnixMilli(10001), byID["active"].StartedAt)
	assert.Equal(t, time.UnixMilli(20001), byID["active"].LastActivity)
	assert.Equal(t, time.UnixMilli(30000), byID["archived"].StartedAt)
	assert.Equal(t, time.UnixMilli(40000), byID["archived"].LastActivity)
}

func TestDiscoverCodexMissingOrIncompatibleIndexReturnsEmpty(t *testing.T) {
	cwd := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	got, err := discoverCodex(missing, cwd)
	require.NoError(t, err)
	assert.Empty(t, got)

	incompatible := filepath.Join(t.TempDir(), "incompatible.sqlite")
	db, err := sql.Open("sqlite", incompatible)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE unrelated (id TEXT)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	got, err = discoverCodex(incompatible, cwd)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDiscoverCodexRejectsNonFileIndex(t *testing.T) {
	_, err := discoverCodex(t.TempDir(), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session index is not a regular file")
}

func TestLoadCodexDetailNormalizesTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeJSONL(t, path,
		`{"timestamp":"2026-07-18T08:00:00Z","type":"session_meta","payload":{"id":"codex-1","cwd":"/workspace/project"}}`,
		`{"timestamp":"2026-07-18T08:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"Inspect the parser"}}`,
		`{"timestamp":"2026-07-18T08:00:02Z","type":"event_msg","payload":{"type":"agent_message","message":"I will inspect it"}}`,
		`{"timestamp":"2026-07-18T08:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I found it"}]}}`,
		`{"timestamp":"2026-07-18T08:00:04Z","type":"response_item","payload":{"type":"function_call","name":"rg","arguments":"{\"pattern\":\"session\"}","call_id":"call-1"}}`,
		`{"timestamp":"2026-07-18T08:00:05Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","arguments":"{}","call_id":"call-2"}}`,
		`{"timestamp":"2026-07-18T08:00:06Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"matched session"}}`,
		`{"timestamp":"2026-07-18T08:00:07Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-2","output":"patched"}}`,
		`{"timestamp":"2026-07-18T08:00:08Z","type":"event_msg","payload":{"type":"agent_message","message":"Done"}}`,
		"not-json",
		`{"timestamp":"2026-07-18T08:00:09Z","type":"response_item","payload":{"type":"future_payload","value":1}}`,
	)

	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	item := model.AgentSession{
		Agent:        "codex",
		ID:           "codex-1",
		StartedAt:    started,
		LastActivity: started.Add(9 * time.Second),
		Path:         path,
	}

	detail, err := loadCodexDetail(item)
	require.NoError(t, err)
	assert.Equal(t, item, detail.Session)
	assert.Equal(t, "/workspace/project", detail.CWD)
	assert.Equal(t, "Inspect the parser", detail.Title)

	require.Len(t, detail.Events, 9)
	assert.Equal(t, "user", detail.Events[0].Role)
	assert.Equal(t, "message", detail.Events[0].Kind)
	assert.Equal(t, "Inspect the parser", detail.Events[0].Summary)
	assert.Equal(t, "assistant", detail.Events[1].Role)
	assert.Equal(t, "I will inspect it", detail.Events[1].Summary)
	assert.Equal(t, "assistant", detail.Events[2].Role)
	assert.Equal(t, "I found it", detail.Events[2].Summary)
	assert.Equal(t, []string{"rg", "apply_patch", "matched session", "patched"}, []string{
		detail.Events[3].Summary,
		detail.Events[4].Summary,
		detail.Events[5].Summary,
		detail.Events[6].Summary,
	})
	for _, event := range detail.Events[3:7] {
		assert.Equal(t, "tool", event.Role)
		assert.Equal(t, "tool_call", event.Kind)
	}
	assert.Equal(t, "assistant", detail.Events[7].Role)
	assert.Equal(t, "Done", detail.Events[7].Summary)
	assert.Equal(t, "raw", detail.Events[8].Kind)
	assert.JSONEq(t, `{"type":"response_item","payload":{"type":"future_payload","value":1},"timestamp":"2026-07-18T08:00:09Z"}`, detail.Events[8].Raw)
}
