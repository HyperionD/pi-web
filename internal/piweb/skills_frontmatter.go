package piweb

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxSkillFileBytes = 1 << 20

var errSkillFrontmatter = errors.New("skill frontmatter is not safely editable")

type skillFrontmatterBlock struct {
	openingEnd   int
	closingStart int
	newline      string
}

type skillLine struct {
	start      int
	end        int
	newlineEnd int
	text       string
}

func skillInvocationEnabled(content []byte) (bool, error) {
	block, found, err := findSkillFrontmatter(content)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	line, value, err := findSkillInvocationLine(content, block)
	if err != nil {
		return false, err
	}
	if line == nil {
		return true, nil
	}
	switch normalizeSkillBoolean(value) {
	case "true":
		return false, nil
	case "false":
		return true, nil
	default:
		return false, fmt.Errorf("%w: disable-model-invocation must be true or false", errSkillFrontmatter)
	}
}

func setSkillInvocation(content []byte, enabled bool) ([]byte, error) {
	block, found, err := findSkillFrontmatter(content)
	if err != nil {
		return nil, err
	}
	if !found {
		if enabled {
			return append([]byte(nil), content...), nil
		}
		return addSkillFrontmatter(content), nil
	}

	line, value, err := findSkillInvocationLine(content, block)
	if err != nil {
		return nil, err
	}
	if enabled {
		if line == nil {
			return append([]byte(nil), content...), nil
		}
		return append(append([]byte(nil), content[:line.start]...), content[line.newlineEnd:]...), nil
	}
	if line == nil {
		insert := []byte("disable-model-invocation: true" + block.newline)
		result := make([]byte, 0, len(content)+len(insert))
		result = append(result, content[:block.openingEnd]...)
		result = append(result, insert...)
		result = append(result, content[block.openingEnd:]...)
		return result, nil
	}

	colon := strings.IndexByte(line.text, ':')
	prefix := []byte(line.text[:colon+1])
	prefix = append(prefix, " true"...)
	result := make([]byte, 0, len(content))
	result = append(result, content[:line.start]...)
	result = append(result, prefix...)
	result = append(result, content[line.end:]...)
	_ = value
	return result, nil
}

func readSkillInvocation(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxSkillFileBytes+1))
	if err != nil {
		return false, err
	}
	if len(content) > maxSkillFileBytes {
		return false, fmt.Errorf("skill file exceeds %d bytes", maxSkillFileBytes)
	}
	return skillInvocationEnabled(content)
}

func writeSkillFileAtomic(path string, content []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("skill path is not a regular file")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pi-web-skill-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func addSkillFrontmatter(content []byte) []byte {
	bom := []byte{}
	body := content
	if len(content) >= 3 && string(content[:3]) == "\ufeff" {
		bom = append(bom, content[:3]...)
		body = content[3:]
	}
	result := make([]byte, 0, len(content)+40)
	result = append(result, bom...)
	result = append(result, "---\ndisable-model-invocation: true\n---\n"...)
	result = append(result, body...)
	return result
}

func findSkillFrontmatter(content []byte) (skillFrontmatterBlock, bool, error) {
	start := 0
	if len(content) >= 3 && string(content[:3]) == "\ufeff" {
		start = 3
	}
	opening, ok := nextSkillLine(content, start)
	if !ok || strings.TrimSpace(opening.text) != "---" {
		return skillFrontmatterBlock{}, false, nil
	}
	if opening.newlineEnd == opening.end {
		return skillFrontmatterBlock{}, false, errSkillFrontmatter
	}
	block := skillFrontmatterBlock{
		openingEnd: opening.newlineEnd,
		newline:    string(content[opening.end:opening.newlineEnd]),
	}
	for cursor := block.openingEnd; cursor < len(content); {
		line, ok := nextSkillLine(content, cursor)
		if !ok {
			break
		}
		if strings.TrimSpace(line.text) == "---" {
			block.closingStart = line.start
			return block, true, nil
		}
		cursor = line.newlineEnd
	}
	return skillFrontmatterBlock{}, false, errSkillFrontmatter
}

func findSkillInvocationLine(content []byte, block skillFrontmatterBlock) (*skillLine, string, error) {
	var match *skillLine
	value := ""
	for cursor := block.openingEnd; cursor < block.closingStart; {
		line, ok := nextSkillLine(content, cursor)
		if !ok || line.start >= block.closingStart {
			break
		}
		key, parsed, isInline := parseSkillKey(line.text)
		if isInline {
			return nil, "", errSkillFrontmatter
		}
		if key {
			if match != nil {
				return nil, "", fmt.Errorf("%w: duplicate disable-model-invocation", errSkillFrontmatter)
			}
			copyLine := line
			match = &copyLine
			value = parsed
		}
		cursor = line.newlineEnd
	}
	return match, value, nil
}

func parseSkillKey(line string) (bool, string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return false, "", true
	}
	if strings.HasPrefix(trimmed, "#") {
		return false, "", false
	}
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return false, "", false
	}
	key := strings.TrimSpace(line[:colon])
	key = strings.Trim(key, "\"'")
	if key != "disable-model-invocation" {
		return false, "", false
	}
	return true, strings.TrimSpace(line[colon+1:]), false
}

func normalizeSkillBoolean(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		value = value[1 : len(value)-1]
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, "#", 2)[0]))
}

func nextSkillLine(content []byte, start int) (skillLine, bool) {
	if start >= len(content) {
		return skillLine{}, false
	}
	end := start
	for end < len(content) && content[end] != '\r' && content[end] != '\n' {
		end++
	}
	newlineEnd := end
	if end < len(content) {
		newlineEnd++
		if content[end] == '\r' && newlineEnd < len(content) && content[newlineEnd] == '\n' {
			newlineEnd++
		}
	}
	return skillLine{start: start, end: end, newlineEnd: newlineEnd, text: string(content[start:end])}, true
}
