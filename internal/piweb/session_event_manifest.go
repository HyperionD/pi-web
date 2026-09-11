package piweb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type ManifestEntry struct {
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	Tool       string `json:"tool,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
	Args       string `json:"args,omitempty"`
	Body       string `json:"body,omitempty"`
	Status     string `json:"status,omitempty"`
}

type RunManifest struct {
	Entries []ManifestEntry `json:"entries"`
}

func BuildRunManifest(items []ProjectedSessionItem) RunManifest {
	manifest := RunManifest{Entries: []ManifestEntry{}}
	for _, item := range items {
		manifest.Entries = append(manifest.Entries, ManifestEntry{
			Kind: item.Kind, Text: item.Text, Tool: item.Tool, ToolCallID: item.ToolCallID,
			Args: item.Args, Body: item.Body, Status: canonicalManifestStatus(item.Status),
		})
	}
	return manifest
}

func canonicalManifestStatus(status string) string {
	switch status {
	case ItemStatusStreaming:
		return ItemStatusCommitted
	default:
		return status
	}
}

func (m RunManifest) Hash() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func ManifestMatches(stored RunManifest, imported []ProjectedSessionItem) bool {
	if len(stored.Entries) == 0 {
		return true
	}
	remaining := append([]ManifestEntry(nil), stored.Entries...)
	for _, item := range imported {
		for index := 0; index < len(remaining); index++ {
			if manifestEntryMatches(remaining[index], item) {
				remaining = append(remaining[:index], remaining[index+1:]...)
				break
			}
		}
	}
	return len(remaining) == 0
}

func manifestEntryMatches(entry ManifestEntry, item ProjectedSessionItem) bool {
	if entry.Kind != item.Kind || entry.Tool != item.Tool {
		return false
	}
	if entry.ToolCallID != "" && entry.ToolCallID != item.ToolCallID {
		return false
	}
	switch entry.Kind {
	case "tool":
		return entry.Args == item.Args && entry.Body == item.Body && entry.Status == canonicalManifestStatus(item.Status)
	default:
		return entry.Text == item.Text
	}
}
