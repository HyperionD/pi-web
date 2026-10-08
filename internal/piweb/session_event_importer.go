package piweb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type SourceFingerprint struct {
	Size        int64
	ModTimeNano int64
	ContentHash string
}

type ImportedSession struct {
	Header      sessionHeader
	Fingerprint SourceFingerprint
	Items       []ProjectedSessionItem
}

func StrictParsePiSession(path string) (ImportedSession, error) {
	before, err := os.Stat(path)
	if err != nil {
		return ImportedSession{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ImportedSession{}, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return ImportedSession{}, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ImportedSession{}, fmt.Errorf("session file changed during import")
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return ImportedSession{}, fmt.Errorf("session file does not end with a newline")
	}
	result := ImportedSession{
		Fingerprint: SourceFingerprint{
			Size:        after.Size(),
			ModTimeNano: after.ModTime().UnixNano(),
			ContentHash: hashBytes(data),
		},
	}
	ordinal := int64(0)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			return ImportedSession{}, fmt.Errorf("malformed session line: %w", err)
		}
		if probe.Type == "session" {
			if err := json.Unmarshal([]byte(line), &result.Header); err != nil {
				return ImportedSession{}, fmt.Errorf("malformed session header: %w", err)
			}
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return ImportedSession{}, fmt.Errorf("malformed session entry: %w", err)
		}
		items, err := importSessionEntry(entry, ordinal)
		if err != nil {
			return ImportedSession{}, err
		}
		for index := range items {
			ordinal++
			items[index].Ordinal = ordinal
			items[index].ItemID = importedItemID(entry.ID, items[index].BlockIndex)
			items[index].Origin = "import"
		}
		result.Items = append(result.Items, items...)
	}
	return result, nil
}

func importSessionEntry(entry sessionEntry, base int64) ([]ProjectedSessionItem, error) {
	switch entry.Type {
	case "message":
		return importAgentMessage(entry)
	default:
		return nil, nil
	}
}

func importAgentMessage(entry sessionEntry) ([]ProjectedSessionItem, error) {
	var message agentMessage
	if err := json.Unmarshal(entry.Message, &message); err != nil {
		return nil, fmt.Errorf("malformed message entry %s: %w", entry.ID, err)
	}
	switch message.Role {
	case "user":
		text := strings.TrimSpace(imageFileTagPattern.ReplaceAllString(contentText(message.Content), ""))
		return []ProjectedSessionItem{{Kind: "user", Text: text, Attachments: contentImageAttachments(message.Content)}}, nil
	case "assistant":
		items, err := importAssistantContent(message.Content)
		if err != nil {
			return nil, err
		}
		if failure := assistantErrorMessage(entry.Message); failure != "" {
			items = append(items, ProjectedSessionItem{
				Kind: "error", Text: failure, BlockIndex: len(items),
			})
		}
		return items, nil
	case "toolResult":
		status := ToolStatusOK
		if message.IsError {
			status = ToolStatusErr
		}
		item := ProjectedSessionItem{
			Kind: "tool", Tool: message.ToolName, ToolCallID: message.ToolCallID, EntityID: "tool:" + message.ToolCallID,
			Body: contentText(message.Content), Status: status, ContentLength: int64(len(contentText(message.Content))),
		}
		return []ProjectedSessionItem{item}, nil
	case "bashExecution":
		status := ToolStatusOK
		if message.ExitCode != nil && *message.ExitCode != 0 {
			status = ToolStatusErr
		}
		if message.Cancelled {
			status = ToolStatusErr
		}
		return []ProjectedSessionItem{{Kind: "tool", Tool: "bash", Args: message.Command, Body: message.Output, Status: status, ContentLength: int64(len(message.Output))}}, nil
	case "custom":
		return []ProjectedSessionItem{{Kind: "pi", Text: contentText(message.Content)}}, nil
	default:
		return nil, nil
	}
}

func importAssistantContent(raw json.RawMessage) ([]ProjectedSessionItem, error) {
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		text := contentText(raw)
		if text == "" {
			return nil, nil
		}
		return []ProjectedSessionItem{{Kind: "pi", Text: text}}, nil
	}
	var items []ProjectedSessionItem
	var text []string
	flush := func() {
		if len(text) == 0 {
			return
		}
		items = append(items, ProjectedSessionItem{Kind: "pi", Text: strings.Join(text, "\n"), BlockIndex: len(items)})
		text = nil
	}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				text = append(text, block.Text)
			}
		case "thinking":
			flush()
			if block.Thinking != "" {
				items = append(items, ProjectedSessionItem{Kind: "think", Text: block.Thinking, BlockIndex: len(items)})
			}
		case "toolCall":
			flush()
			if block.Name != "" {
				items = append(items, ProjectedSessionItem{Kind: "tool", Tool: block.Name, ToolCallID: block.ID, Args: string(block.Arguments), Status: ToolStatusOK, BlockIndex: len(items)})
			}
		}
	}
	flush()
	return items, nil
}

func importedItemID(entryID string, blockIndex int) string {
	if entryID == "" {
		entryID = "entry"
	}
	return "import:" + entryID + ":" + strconv.Itoa(blockIndex)
}

func hashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
