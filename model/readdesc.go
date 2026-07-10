package model

import (
	"os"
	"strings"
)

// descMaxChars bounds the description preview the renderer shows per
// skill/subagent. If SKILL.md's body line exceeds this, ReadDescription
// truncates it and appends "..." so a single-line preview still fits.
const descMaxChars = 60

// ReadDescription returns the first non-empty, non-heading line of path
// (treated as a markdown file), trimmed and truncated to descMaxChars
// runes. Returns "" if the file is unreadable, empty, or all headings.
//
// If the file starts with YAML frontmatter, the function first looks for
// a "description:" key inside that frontmatter and returns its value
// instead. Block-scalar values (">" or "|") and the "- >" folded form are
// assembled from subsequent indented lines until an unindented line ends
// the block. The same parser is used by svc/plugin/manifest.go's
// scanSkills / scanSubagents and by svc/agent/installed.go's discovery —
// keeping it in one place means a description rendered from a SKILL.md on
// disk matches what the same file would have rendered at install time.
func ReadDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)

	// YAML frontmatter form: extract description: key directly.
	if strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n") {
		lines := strings.Split(content, "\n")
		var fmLines []string
		inFM := false
		for i, line := range lines {
			lineTrim := strings.TrimSpace(line)
			if i == 0 && lineTrim == "---" {
				inFM = true
				continue
			}
			if inFM && lineTrim == "---" {
				inFM = false
				break
			}
			if inFM {
				fmLines = append(fmLines, line)
			}
		}
		for j, fmLine := range fmLines {
			fmLineTrim := strings.TrimSpace(fmLine)
			if strings.HasPrefix(strings.ToLower(fmLineTrim), "description:") {
				val := strings.TrimSpace(strings.TrimPrefix(fmLineTrim, fmLineTrim[:12]))
				var descLines []string
				isFoldedOrLiteral := val == ">" || val == "|" || val == ">-" || val == "|-"
				if isFoldedOrLiteral || val == "" {
					for k := j + 1; k < len(fmLines); k++ {
						nextVal := fmLines[k]
						if strings.TrimSpace(nextVal) == "" {
							descLines = append(descLines, "")
							continue
						}
						if strings.HasPrefix(nextVal, " ") || strings.HasPrefix(nextVal, "\t") {
							descLines = append(descLines, strings.TrimSpace(nextVal))
						} else {
							break
						}
					}
				} else {
					descLines = append(descLines, val)
				}

				desc := strings.Join(descLines, " ")
				for strings.Contains(desc, "  ") {
					desc = strings.ReplaceAll(desc, "  ", " ")
				}
				desc = strings.TrimSpace(desc)

				if len(desc) >= 2 {
					if (desc[0] == '"' && desc[len(desc)-1] == '"') || (desc[0] == '\'' && desc[len(desc)-1] == '\'') {
						desc = desc[1 : len(desc)-1]
					}
				}
				if desc != "" {
					return desc
				}
			}
		}
	}

	// Fallback: first non-empty, non-heading line.
	lines := strings.Split(content, "\n")
	inFM := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if i == 0 && line == "---" {
			inFM = true
			continue
		}
		if inFM {
			if line == "---" {
				inFM = false
			}
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line == "---" || line == "===" {
			continue
		}
		return line
	}
	return ""
}