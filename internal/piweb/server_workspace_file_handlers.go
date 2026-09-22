package piweb

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) workspaceFileAsset(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	rel := r.URL.Query().Get("path")
	full, err := SafeJoin(root, rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, err := openWorkspaceFileAsset(root, rel)
	if err != nil {
		switch {
		case errors.Is(err, ErrSafeUploadUnsupported):
			writeError(w, http.StatusNotImplemented, err)
		case errors.Is(err, ErrUnsafeWorkspacePath):
			writeError(w, http.StatusForbidden, ErrWorkspaceForbidden)
		case errors.Is(err, os.ErrNotExist):
			writeError(w, http.StatusNotFound, err)
		default:
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, errors.New("path is a directory"))
		return
	}
	probe := make([]byte, 512)
	n, readErr := file.Read(probe)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		writeError(w, http.StatusInternalServerError, readErr)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	contentType := detectPreviewMIME(full, probe[:n])
	download := r.URL.Query().Get("download") == "1"
	if !download && strings.HasPrefix(contentType, "image/") && info.Size() > 10*1024*1024 {
		writeError(w, http.StatusRequestEntityTooLarge, errors.New("image is too large for preview (>10MB)"))
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fileContentDisposition(info.Name(), download))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType == "text/html" || contentType == "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'")
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func fileContentDisposition(name string, download bool) string {
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	base := filepath.Base(name)
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune("\\\";", r) {
			return '_'
		}
		return r
	}, base)
	if fallback == "" || fallback == "." || fallback == ".." {
		fallback = "download"
	}
	return disposition + `; filename="` + fallback + `"; filename*=UTF-8''` + url.PathEscape(base)
}

func (s *Server) readWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	offset, limit, err := parseFileReadQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var file FileContent
	if offset == 0 && limit == 256*1024 {
		file, err = s.store.ReadFile(r.PathValue("workspaceID"), path)
	} else {
		file, err = s.store.ReadFileChunk(r.PathValue("workspaceID"), path, offset, limit)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func parseFileReadQuery(r *http.Request) (int64, int64, error) {
	const defaultLimit int64 = 256 * 1024
	parse := func(name string, fallback int64) (int64, error) {
		value := r.URL.Query().Get(name)
		if value == "" {
			return fallback, nil
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return 0, errors.New("invalid " + name)
		}
		return parsed, nil
	}
	offset, err := parse("offset", 0)
	if err != nil {
		return 0, 0, err
	}
	limit, err := parse("limit", defaultLimit)
	if err != nil || limit == 0 || limit > defaultLimit {
		return 0, 0, errors.New("invalid limit")
	}
	return offset, limit, nil
}
func (s *Server) writeWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	var req WriteFileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, err := s.store.WriteFile(r.PathValue("workspaceID"), r.URL.Query().Get("path"), req.Content)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}
func (s *Server) createWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	var req CreateFileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, err := s.store.CreateFile(r.PathValue("workspaceID"), req.Path, req.Kind, req.Content)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}
func (s *Server) renameWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	var req RenameFileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.RenameFile(r.PathValue("workspaceID"), req.OldPath, req.NewPath); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"renamed": true, "path": req.NewPath})
}
func (s *Server) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	var req DeleteFileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.DeleteFile(r.PathValue("workspaceID"), req.Path); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
func (s *Server) uploadWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	maxBytes, err := LoadMaxUploadBytes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && mediaType == "multipart/form-data" {
		s.uploadMultipartWorkspaceFile(w, r, maxBytes)
		return
	}

	var req UploadFileRequest
	if err := readJSONWithLimit(r, &req, maxUploadJSONBodyBytes(maxBytes)); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			writeUploadTooLarge(w, maxBytes)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	content := req.Content
	if index := strings.Index(content, ","); strings.HasPrefix(content, "data:") && index >= 0 {
		content = content[index+1:]
	}
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if int64(len(data)) > maxBytes {
		writeUploadTooLarge(w, maxBytes)
		return
	}
	file, err := s.store.UploadFile(r.PathValue("workspaceID"), req.Path, data, req.Overwrite)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}
