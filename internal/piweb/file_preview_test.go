package piweb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewKindForMediaAndHTMLMIME(t *testing.T) {
	cases := map[string]string{
		"image/png":       "image",
		"audio/mpeg":      "audio",
		"video/mp4":       "video",
		"application/pdf": "pdf",
		"text/html":       "html",
		"text/plain":      "text",
		"application/zip": "unsupported",
	}
	for mimeType, want := range cases {
		if got := previewKindForMIME(mimeType); got != want {
			t.Errorf("previewKindForMIME(%q) = %q, want %q", mimeType, got, want)
		}
	}
}

func TestReadWorkspaceFileChunkKeepsUTF8AndReportsNextOffset(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("a", 256*1024-1) + "😀tail"
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := ReadWorkspaceFileChunk(root, "notes.txt", 0, 256*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(first.Content, "a") {
		t.Fatalf("first chunk ended with an invalid boundary: %q", first.Content[len(first.Content)-8:])
	}
	if first.NextOffset <= 0 || first.NextOffset >= int64(len(body)) {
		t.Fatalf("unexpected next offset: %d", first.NextOffset)
	}
	if first.Content != body[:first.NextOffset] {
		t.Fatal("first chunk did not preserve the original byte sequence")
	}
	if first.Size != int64(len(body)) || !first.Truncated || first.Version == "" {
		t.Fatalf("unexpected chunk metadata: %#v", first)
	}

	second, err := ReadWorkspaceFileChunk(root, "notes.txt", first.NextOffset, 256*1024)
	if err != nil {
		t.Fatal(err)
	}
	if second.Content != body[first.NextOffset:] || second.Truncated || second.NextOffset != 0 {
		t.Fatalf("unexpected final chunk: %#v", second)
	}
}

func TestReadWorkspaceFileChunkRejectsInvalidOffset(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int64{-1, 6} {
		if _, err := ReadWorkspaceFileChunk(root, "note.txt", offset, 10); err == nil {
			t.Fatalf("offset %d was accepted", offset)
		}
	}
}
