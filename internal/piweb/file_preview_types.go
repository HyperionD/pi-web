package piweb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func fileVersion(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
}

func previewLanguage(path, mimeType string) string {
	if mimeType == "application/json" {
		return "json"
	}
	if mimeType == "text/html" {
		return "html"
	}
	if mimeType == "text/markdown" {
		return "markdown"
	}
	return strings.TrimPrefix(filepath.Ext(path), ".")
}
