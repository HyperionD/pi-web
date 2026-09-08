package piweb

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReadWorkspaceFile(root, rel string, maxBytes int64) (FileContent, error) {
	directory, cleanRel, err := openRootedWorkspacePath(root, rel)
	if err != nil {
		return FileContent{}, err
	}
	defer directory.Close()
	file, err := directory.Open(cleanRel)
	if err != nil {
		return FileContent{}, err
	}
	defer file.Close()
	return readWorkspaceFileFromHandle(root, rel, filepath.Join(root, cleanRel), file, maxBytes)
}

func readWorkspaceFileFromHandle(root, rel, displayPath string, file *os.File, maxBytes int64) (FileContent, error) {
	info, err := file.Stat()
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, errors.New("path is a directory")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return FileContent{}, err
	}
	limit := maxBytes
	if limit <= 0 {
		limit = 256 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return FileContent{}, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	mimeType := detectPreviewMIME(displayPath, data)
	previewKind := previewKindForMIME(mimeType)
	content := ""
	dataURL := ""
	if previewKind == "text" || mimeType == "image/svg+xml" {
		content = string(data)
	}
	if previewKind == "image" && !truncated {
		encoded := base64.StdEncoding.EncodeToString(data)
		dataURL = "data:" + mimeType + ";base64," + encoded
	}
	if previewKind == "image" && truncated {
		previewKind = "unsupported"
	}
	cleanRel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(rel, "/")))
	originalContent, gitStatus := GitOriginalFileContent(root, cleanRel)
	return FileContent{
		Path:            cleanRel,
		Content:         content,
		OriginalContent: originalContent,
		GitStatus:       gitStatus,
		DataURL:         dataURL,
		MIME:            mimeType,
		PreviewKind:     previewKind,
		Truncated:       truncated,
	}, nil
}

func WriteWorkspaceFile(root, rel, content string) (FileContent, error) {
	directory, cleanRel, err := openRootedWorkspacePath(root, rel)
	if err != nil {
		return FileContent{}, err
	}
	defer directory.Close()
	info, err := directory.Stat(cleanRel)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, errors.New("path is a directory")
	}
	if err := directory.WriteFile(cleanRel, []byte(content), 0o600); err != nil {
		return FileContent{}, err
	}
	return ReadWorkspaceFile(root, rel, 256*1024)
}

func CreateWorkspacePath(root, rel, kind, content string) (FileContent, error) {
	directory, cleanRel, err := openRootedWorkspacePath(root, rel)
	if err != nil {
		return FileContent{}, err
	}
	defer directory.Close()
	if _, err := directory.Stat(cleanRel); err == nil {
		return FileContent{}, errors.New("path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileContent{}, err
	}
	if err := directory.MkdirAll(filepath.Dir(cleanRel), 0o700); err != nil {
		return FileContent{}, err
	}
	if kind == "dir" {
		if err := directory.Mkdir(cleanRel, 0o700); err != nil {
			return FileContent{}, err
		}
		return FileContent{Path: filepath.ToSlash(filepath.Clean(rel)), PreviewKind: "unsupported"}, nil
	}
	if kind != "file" {
		return FileContent{}, errors.New("kind must be file or dir")
	}
	if err := directory.WriteFile(cleanRel, []byte(content), 0o600); err != nil {
		return FileContent{}, err
	}
	return ReadWorkspaceFile(root, rel, 256*1024)
}

func RenameWorkspacePath(root, oldRel, newRel string) error {
	directory, oldPath, err := openRootedWorkspacePath(root, oldRel)
	if err != nil {
		return err
	}
	defer directory.Close()
	newFull, err := SafeJoin(root, newRel)
	if err != nil {
		return err
	}
	newPath, err := filepath.Rel(root, newFull)
	if err != nil {
		return err
	}
	if _, err := directory.Lstat(oldPath); err != nil {
		return err
	}
	if _, err := directory.Lstat(newPath); err == nil {
		return errors.New("target already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := directory.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
		return err
	}
	return directory.Rename(oldPath, newPath)
}

func DeleteWorkspacePath(root, rel string) error {
	directory, cleanRel, err := openRootedWorkspacePath(root, rel)
	if err != nil {
		return err
	}
	defer directory.Close()
	if _, err := directory.Stat(cleanRel); err != nil {
		return err
	}
	return directory.RemoveAll(cleanRel)
}
