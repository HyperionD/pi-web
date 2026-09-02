package piweb

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

const (
	defaultSessionMessageLimit = 120
	maxSessionMessageLimit     = 500
	sessionTailChunkBytes      = 64 * 1024
)

type SessionMessagePage struct {
	Messages []Message `json:"messages"`
	Cursor   string    `json:"cursor,omitempty"`
	HasMore  bool      `json:"hasMore"`
	Limit    int       `json:"limit"`
}

func normalizeSessionMessageLimit(limit int) int {
	if limit <= 0 {
		return defaultSessionMessageLimit
	}
	if limit > maxSessionMessageLimit {
		return maxSessionMessageLimit
	}
	return limit
}

func ParsePiSessionMessagePage(path string, limit int, before string) (SessionMessagePage, error) {
	limit = normalizeSessionMessageLimit(limit)
	file, err := os.Open(path)
	if err != nil {
		return SessionMessagePage{}, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return SessionMessagePage{}, err
	}
	end := sessionPageEndOffset(stat.Size(), before)
	messages, cursor, hasMore, err := tailSessionMessages(file, end, limit)
	if err != nil {
		return SessionMessagePage{}, err
	}
	return SessionMessagePage{
		Messages: messages,
		Cursor:   cursor,
		HasMore:  hasMore,
		Limit:    limit,
	}, nil
}

func sessionPageEndOffset(size int64, before string) int64 {
	if before == "" {
		return size
	}
	offset, err := strconv.ParseInt(before, 10, 64)
	if err != nil || offset < 0 {
		return size
	}
	if offset > size {
		return size
	}
	return offset
}

type compactionTailBoundary struct {
	active              bool
	firstKeptID         string
	compactionEndOffset int64
	summary             []Message
}

func tailSessionMessages(file *os.File, end int64, limit int) ([]Message, string, bool, error) {
	var selected []Message
	var prefix []byte
	oldestOffset := end
	position := end
	boundary := compactionTailBoundary{}
	compactionBoundary := false
	for position > 0 && len(selected) < limit {
		readSize := int64(sessionTailChunkBytes)
		if position < readSize {
			readSize = position
		}
		start := position - readSize
		chunk := make([]byte, readSize)
		if _, err := file.ReadAt(chunk, start); err != nil {
			return nil, "", false, err
		}
		data := append(chunk, prefix...)
		complete, nextPrefix, baseOffset, startsAtFileStart := completeTailData(data, start)
		prefix = nextPrefix
		_ = startsAtFileStart
		var reachedBoundary bool
		selected, oldestOffset, reachedBoundary = collectTailMessages(
			complete, baseOffset, selected, &oldestOffset, limit, &boundary,
		)
		if reachedBoundary {
			compactionBoundary = true
			break
		}
		position = start
	}
	if boundary.active && position == 0 {
		if len(selected)+len(boundary.summary) <= limit {
			selected = append(boundary.summary, selected...)
			compactionBoundary = true
		} else {
			compactionBoundary = false
		}
	}
	hasMore := !compactionBoundary && (position > 0 || (len(selected) >= limit && oldestOffset > 0))
	return selected, strconv.FormatInt(oldestOffset, 10), hasMore, nil
}

func completeTailData(data []byte, start int64) ([]byte, []byte, int64, bool) {
	if start == 0 {
		return data, nil, 0, true
	}
	newline := bytes.IndexByte(data, '\n')
	if newline < 0 {
		return nil, data, start, false
	}
	return data[newline+1:], data[:newline], start + int64(newline+1), false
}

func collectTailMessages(
	data []byte,
	baseOffset int64,
	selected []Message,
	oldestOffset *int64,
	limit int,
	boundary *compactionTailBoundary,
) ([]Message, int64, bool) {
	lineEnd := len(data)
	for lineEnd > 0 && len(selected) < limit {
		lineStart := bytes.LastIndexByte(data[:lineEnd], '\n') + 1
		line := strings.TrimSpace(string(data[lineStart:lineEnd]))
		lineOffset := baseOffset + int64(lineStart)
		entryType, entryID, firstKeptID := sessionLineIdentity(line)
		if boundary.active {
			if entryType == "message" {
				if messages := ParsePiSessionLineMessages(line); len(messages) > 0 {
					selected = append(messages, selected...)
					*oldestOffset = lineOffset
				}
				if entryID == boundary.firstKeptID {
					if len(selected)+len(boundary.summary) <= limit {
						selected = append(boundary.summary, selected...)
						return selected, *oldestOffset, true
					}
					*oldestOffset = boundary.compactionEndOffset
					return selected, *oldestOffset, false
				}
			}
			lineEnd = lineStart - 1
			continue
		}
		if entryType == "compaction" {
			boundary.summary = ParsePiSessionLineMessages(line)
			*oldestOffset = lineOffset
			boundary.active = true
			boundary.compactionEndOffset = baseOffset + int64(lineEnd+1)
			boundary.firstKeptID = firstKeptID
			if boundary.firstKeptID == "" {
				selected = append(boundary.summary, selected...)
				return selected, *oldestOffset, true
			}
			lineEnd = lineStart - 1
			continue
		}
		if messages := ParsePiSessionLineMessages(line); len(messages) > 0 {
			selected = append(messages, selected...)
			*oldestOffset = lineOffset
		}
		lineEnd = lineStart - 1
	}
	return selected, *oldestOffset, false
}

func sessionLineIdentity(line string) (string, string, string) {
	var entry struct {
		Type           string `json:"type"`
		ID             string `json:"id"`
		FirstKeptEntry string `json:"firstKeptEntryId"`
	}
	if json.Unmarshal([]byte(line), &entry) != nil {
		return "", "", ""
	}
	return entry.Type, entry.ID, entry.FirstKeptEntry
}
