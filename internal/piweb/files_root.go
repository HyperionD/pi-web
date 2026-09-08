package piweb

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

func rootedFileContentContains(root, rel, needle string) bool {
	directory, cleanRel, err := openRootedWorkspacePath(root, rel)
	if err != nil {
		return false
	}
	defer directory.Close()
	file, err := directory.Open(cleanRel)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSearchFileBytes))
	if err != nil {
		return false
	}
	mimeType := detectPreviewMIME(rel, data)
	if previewKindForMIME(mimeType) != "text" && mimeType != "image/svg+xml" {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), needle)
}

// os.Root confines filesystem operations even if a path component is replaced by a symlink.
func openRootedWorkspacePath(root, rel string) (*os.Root, string, error) {
	full, err := SafeJoin(root, rel)
	if err != nil {
		return nil, "", err
	}
	cleanRel, err := filepath.Rel(root, full)
	if err != nil {
		return nil, "", err
	}
	directory, err := os.OpenRoot(root)
	return directory, cleanRel, err
}
