package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var (
	codexToolRegex     = regexp.MustCompile(`tools\.([a-zA-Z0-9_]+)`)
	antigravityModelRe = regexp.MustCompile(`Model Selection. from [^ ]+ to ([a-zA-Z0-9. ()\-+]+)`)
)

func fallsOnDate(t time.Time, targetDate string, loc *time.Location) bool {
	return t.In(loc).Format("2006-01-02") == targetDate
}

func getHourString(t time.Time, loc *time.Location) string {
	return fmt.Sprintf("%d", t.In(loc).Hour())
}

func parseTime(raw any) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}
	switch val := raw.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, val)
		if err == nil {
			return t, true
		}
		t, err = time.Parse("2006-01-02T15:04:05.000Z", val)
		if err == nil {
			return t, true
		}
		// Try custom formats
		t, err = time.Parse("2006-01-02 15:04:05", val)
		if err == nil {
			return t, true
		}
	case float64:
		return time.UnixMilli(int64(val)), true
	}
	return time.Time{}, false
}

func extractSkillName(path string) string {
	idx := strings.Index(path, "skills/")
	if idx == -1 {
		return ""
	}
	sub := path[idx+len("skills/"):]
	if len(sub) == 0 {
		return ""
	}
	parts := strings.Split(sub, "/")
	if len(parts) > 0 {
		name := parts[0]
		if strings.HasSuffix(name, ".md") {
			name = strings.TrimSuffix(name, ".md")
		}
		return name
	}
	return ""
}

// ParseClaudeLogs parses Claude Code project session logs for a given date.
func ParseClaudeLogs(ds *DayStats, targetDate string, loc *time.Location) error {
	projectsDir := expandPath(viper.GetString("sources.claude.projects_dir"))
	targetStart, err := time.ParseInLocation("2006-01-02", targetDate, loc)
	if err != nil {
		return err
	}

	err = filepath.Walk(projectsDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}

		// Optimization: if modified before target day start, skip
		if info.ModTime().In(loc).Before(targetStart) {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		// 10MB scanner buffer limit
		scanner.Buffer(make([]byte, 1<<20), 10<<20)

		for scanner.Scan() {
			line := scanner.Bytes()
			var rawMap map[string]any
			if err := json.Unmarshal(line, &rawMap); err != nil {
				continue
			}

			rawTS := rawMap["timestamp"]
			t, ok := parseTime(rawTS)
			if !ok || !fallsOnDate(t, targetDate, loc) {
				continue
			}

			hourStr := getHourString(t, loc)

			// Check type
			lineType, _ := rawMap["type"].(string)
			if lineType == "assistant" {
				// Parse assistant model and usage
				msg, _ := rawMap["message"].(map[string]any)
				if msg == nil {
					continue
				}

				modelName, _ := msg["model"].(string)
				if modelName == "" || modelName == "<synthetic>" {
					modelName = "claude-code"
				}

				u := ds.Hourly[hourStr].GetOrCreate("claude-code", modelName)

				// Sum tokens
				var inputTokens, outputTokens int64
				if usage, ok := msg["usage"].(map[string]any); ok {
					if in, ok := usage["input_tokens"].(float64); ok {
						inputTokens += int64(in)
					}
					if cacheCreate, ok := usage["cache_creation_input_tokens"].(float64); ok {
						inputTokens += int64(cacheCreate)
					}
					if cacheRead, ok := usage["cache_read_input_tokens"].(float64); ok {
						inputTokens += int64(cacheRead)
					}
					if out, ok := usage["output_tokens"].(float64); ok {
						outputTokens += int64(out)
					}
				}
				u.AddTokens(inputTokens, outputTokens)

				// Parse content for tools and skills
				if contentList, ok := msg["content"].([]any); ok {
					for _, item := range contentList {
						cMap, ok := item.(map[string]any)
						if !ok {
							continue
						}
						cType, _ := cMap["type"].(string)
						if cType == "tool_use" {
							toolName, _ := cMap["name"].(string)
							if toolName != "" {
								u.AddTool(toolName, 1)

								// Extract skill from tool input filepath
								if inputMap, ok := cMap["input"].(map[string]any); ok {
									for _, val := range inputMap {
										if valStr, ok := val.(string); ok {
											skill := extractSkillName(valStr)
											if skill != "" {
												u.AddSkill(skill, 1)
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ParseCodexLogs parses Codex session and archived rollout logs.
func ParseCodexLogs(ds *DayStats, targetDate string, loc *time.Location) error {
	sessionsDir := expandPath(viper.GetString("sources.codex.sessions_dir"))
	archivedDir := expandPath(viper.GetString("sources.codex.archived_dir"))
	_, err := time.ParseInLocation("2006-01-02", targetDate, loc)
	if err != nil {
		return err
	}

	// 1. Scan YYYY/MM/DD session folder directly
	parsedDate, err := time.Parse("2006-01-02", targetDate)
	if err == nil {
		dateSubdir := filepath.Join(sessionsDir, parsedDate.Format("2006/01/02"))
		_ = filepath.Walk(dateSubdir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return nil
			}
			_ = parseCodexFile(path, ds, targetDate, loc)
			return nil
		})
	}

	// 2. Scan archived rollouts that might be on/after target day
	targetStart, _ := time.ParseInLocation("2006-01-02", targetDate, loc)
	_ = filepath.Walk(archivedDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		// If filename matches rollout-YYYY-MM-DD, or mod time is on/after targetStart
		filename := filepath.Base(path)
		if strings.HasPrefix(filename, "rollout-"+targetDate) || !info.ModTime().In(loc).Before(targetStart) {
			_ = parseCodexFile(path, ds, targetDate, loc)
		}
		return nil
	})

	return nil
}

func parseCodexFile(path string, ds *DayStats, targetDate string, loc *time.Location) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 10<<20)

	currentModel := "codex"

	for scanner.Scan() {
		line := scanner.Bytes()
		var rawMap map[string]any
		if err := json.Unmarshal(line, &rawMap); err != nil {
			continue
		}

		rawTS := rawMap["timestamp"]
		t, ok := parseTime(rawTS)
		if !ok || !fallsOnDate(t, targetDate, loc) {
			continue
		}

		hourStr := getHourString(t, loc)

		lineType, _ := rawMap["type"].(string)

		// Model setting detection
		if lineType == "turn_context" {
			if payload, ok := rawMap["payload"].(map[string]any); ok {
				if m, ok := payload["model"].(string); ok && m != "" {
					currentModel = m
				}
			}
		}

		// Token count detection
		if lineType == "event_msg" {
			if payload, ok := rawMap["payload"].(map[string]any); ok {
				pType, _ := payload["type"].(string)
				if pType == "token_count" {
					if info, ok := payload["info"].(map[string]any); ok {
						if lastUsage, ok := info["last_token_usage"].(map[string]any); ok {
							var input, output int64
							if in, ok := lastUsage["input_tokens"].(float64); ok {
								input += int64(in)
							}
							if cached, ok := lastUsage["cached_input_tokens"].(float64); ok {
								input += int64(cached)
							}
							if out, ok := lastUsage["output_tokens"].(float64); ok {
								output += int64(out)
							}
							ds.Hourly[hourStr].GetOrCreate("codex", currentModel).AddTokens(input, output)
						}
					}
				}
			}
		}

		// Tools & Skills detection via regex/path search
		lineStr := string(line)
		matches := codexToolRegex.FindAllStringSubmatch(lineStr, -1)
		if len(matches) > 0 {
			u := ds.Hourly[hourStr].GetOrCreate("codex", currentModel)
			for _, match := range matches {
				u.AddTool(match[1], 1)
			}
		}

		skillName := extractSkillName(lineStr)
		if skillName != "" {
			ds.Hourly[hourStr].GetOrCreate("codex", currentModel).AddSkill(skillName, 1)
		}
	}
	return nil
}

// ParseAntigravityLogs parses Antigravity session transcript logs.
func ParseAntigravityLogs(ds *DayStats, targetDate string, loc *time.Location) error {
	brainDir := expandPath(viper.GetString("sources.antigravity.brain_dir"))
	targetStart, err := time.ParseInLocation("2006-01-02", targetDate, loc)
	if err != nil {
		return err
	}

	err = filepath.Walk(brainDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() || info.Name() != "transcript.jsonl" {
			return nil
		}

		// Optimization: if modified before target day start, skip
		if info.ModTime().In(loc).Before(targetStart) {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 10<<20)

		currentModel := "antigravity"

		for scanner.Scan() {
			line := scanner.Bytes()
			var rawMap map[string]any
			if err := json.Unmarshal(line, &rawMap); err != nil {
				continue
			}

			// Parse timestamp
			rawTS := rawMap["created_at"]
			t, ok := parseTime(rawTS)
			if !ok || !fallsOnDate(t, targetDate, loc) {
				continue
			}

			hourStr := getHourString(t, loc)

			// Try to detect model from settings changes in text
			lineContent, _ := rawMap["content"].(string)
			if mMatch := antigravityModelRe.FindStringSubmatch(lineContent); len(mMatch) > 1 {
				currentModel = strings.TrimSpace(mMatch[1])
			}

			source, _ := rawMap["source"].(string)
			lineType, _ := rawMap["type"].(string)

			u := ds.Hourly[hourStr].GetOrCreate("antigravity", currentModel)

			// Estimate tokens
			var inputTokens, outputTokens int64
			if source == "MODEL" && lineType == "PLANNER_RESPONSE" {
				thinking, _ := rawMap["thinking"].(string)
				// Output tokens estimation: approx. 3 chars per token
				outputTokens = int64(len(lineContent)+len(thinking)) / 3
			} else if source == "USER_EXPLICIT" {
				// Input tokens estimation: approx. 3 chars per token
				inputTokens = int64(len(lineContent)) / 3
			}
			if inputTokens > 0 || outputTokens > 0 {
				u.AddTokens(inputTokens, outputTokens)
			}

			// Parse tools & skills in tool_calls
			if toolCallsRaw, ok := rawMap["tool_calls"].([]any); ok {
				for _, callRaw := range toolCallsRaw {
					call, ok := callRaw.(map[string]any)
					if !ok {
						continue
					}
					toolName, _ := call["name"].(string)
					if toolName != "" {
						u.AddTool(toolName, 1)

						// Convert args to JSON/string to search for skill folders
						if args, ok := call["args"].(map[string]any); ok {
							for _, argVal := range args {
								if argValStr, ok := argVal.(string); ok {
									skill := extractSkillName(argValStr)
									if skill != "" {
										u.AddSkill(skill, 1)
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
