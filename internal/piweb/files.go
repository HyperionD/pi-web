package piweb

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxSearchFileBytes = 256 * 1024

var ErrSafeUploadUnsupported = errors.New("safe upload operations are unavailable on this platform")
var ErrUnsafeWorkspacePath = errors.New("workspace path is not a safe regular path")

func RealFileTree(root string, maxDepth int) ([]FileNode, error) {
	root = filepath.Clean(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	return fileNodes(root, root, entries, 0, maxDepth), nil
}

func fileNodes(root, parent string, entries []os.DirEntry, depth, maxDepth int) []FileNode {
	var nodes []FileNode
	for _, entry := range entries {
		name := entry.Name()
		if shouldSkipFile(name) {
			continue
		}
		path := filepath.Join(parent, name)
		rel, _ := filepath.Rel(root, path)
		node := FileNode{Name: name, Path: filepath.ToSlash(rel), Depth: depth}
		if entry.IsDir() {
			node.Type = "dir"
			if depth < maxDepth {
				if children, err := os.ReadDir(path); err == nil {
					node.Children = fileNodes(root, path, children, depth+1, maxDepth)
				}
			}
		} else {
			node.Type = "file"
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func shouldSkipFile(name string) bool {
	if name == ".git" || name == "node_modules" || name == "dist" || name == ".astro" {
		return true
	}
	return false
}

func ListWorkspaceFiles(root, rel string) ([]WorkspaceFileEntry, error) {
	root = filepath.Clean(root)
	trimmed := strings.Trim(strings.TrimSpace(rel), "/")
	var directory *os.Root
	cleanRel := "."
	var err error
	if trimmed == "" || trimmed == "." {
		directory, err = os.OpenRoot(root)
	} else {
		directory, cleanRel, err = openRootedWorkspacePath(root, trimmed)
	}
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	folder, err := directory.Open(cleanRel)
	if err != nil {
		return nil, err
	}
	defer folder.Close()
	info, err := folder.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("path is not a directory")
	}
	entries, err := folder.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := make([]WorkspaceFileEntry, 0, len(entries))
	for _, entry := range entries {
		if shouldSkipFile(entry.Name()) {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath := filepath.ToSlash(filepath.Join(trimmed, entry.Name()))
		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		}
		result = append(result, WorkspaceFileEntry{
			Name:     entry.Name(),
			Path:     entryPath,
			Type:     entryType,
			Size:     entryInfo.Size(),
			Modified: entryInfo.ModTime().UTC().Format(time.RFC3339Nano),
		})
	}
	return result, nil
}

func SearchWorkspaceFiles(root, query string) ([]string, error) {
	root = filepath.Clean(root)
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return []string{}, nil
	}
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if shouldSkipFile(name) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(strings.ToLower(rel), needle) || rootedFileContentContains(root, rel, needle) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}
