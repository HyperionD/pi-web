package piweb

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC()})
}
func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "~"
	}
	purpose := r.URL.Query().Get("purpose")
	if purpose == "" {
		purpose = folderPurposeWorkspace
	}
	if purpose != folderPurposeWorkspace && purpose != folderPurposeAllowedRoot {
		writeError(w, http.StatusBadRequest, errors.New("invalid folder purpose"))
		return
	}
	var policy WorkspacePolicy
	if purpose == folderPurposeWorkspace {
		var err error
		policy, err = LoadWorkspacePolicy()
		if err != nil {
			writeWorkspaceSelectionError(w, err)
			return
		}
	}
	if policy.restricted {
		if err := policy.CheckFolderPath(path); err != nil {
			if errors.Is(err, ErrWorkspaceForbidden) {
				listing, listingErr := policy.AllowedRootListing(path)
				if listingErr != nil {
					writeWorkspaceSelectionError(w, listingErr)
					return
				}
				writeJSON(w, http.StatusOK, listing)
				return
			}
			writeWorkspaceSelectionError(w, err)
			return
		}
	}
	folders, err := ListFolders(path)
	if err != nil {
		if policy.restricted {
			writeWorkspaceSelectionError(w, err)
		} else {
			writeError(w, http.StatusBadRequest, err)
		}
		return
	}
	if policy.restricted {
		folders, err = policy.FilterFolderListing(folders)
		if err != nil {
			writeWorkspaceSelectionError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, folders)
}
func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	policy, err := LoadWorkspacePolicy()
	if err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	workspaces := s.store.Workspaces()
	if policy.restricted {
		filtered := make([]Workspace, 0, len(workspaces))
		for _, workspace := range workspaces {
			if err := policy.CheckExistingDirectory(workspace.Path); err != nil {
				if errors.Is(err, ErrWorkspaceForbidden) {
					continue
				}
				writeWorkspaceSelectionError(w, err)
				return
			}
			filtered = append(filtered, workspace)
		}
		workspaces = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": s.withLiveSessions(workspaces)})
}
func (s *Server) openWorkspace(w http.ResponseWriter, r *http.Request) {
	var req OpenWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	policy, err := LoadWorkspacePolicy()
	if err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	if err := policy.CheckExistingDirectory(req.Path); err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	workspace, err := s.store.OpenWorkspace(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}
func (s *Server) cloneWorkspace(w http.ResponseWriter, r *http.Request) {
	var req CloneWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	policy, err := LoadWorkspacePolicy()
	if err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	workspace, output, err := CloneGitWorkspace(s.context(), s.store, req, policy)
	if err != nil {
		message := errors.New(strings.TrimSpace(err.Error() + "\n" + output))
		writeError(w, workspaceSelectionErrorStatus(err), message)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workspace": workspace, "output": output})
}
func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWorkspace(r.PathValue("workspaceID")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.store.CreateSession(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": session})
}
func (s *Server) workspaceSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.Sessions(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.withLiveSessionFlags(sessions)})
}
func (s *Server) withLiveSessions(workspaces []Workspace) []Workspace {
	for workspaceIndex := range workspaces {
		workspaces[workspaceIndex].Sessions = s.withLiveSessionFlags(workspaces[workspaceIndex].Sessions)
		for _, session := range workspaces[workspaceIndex].Sessions {
			workspaces[workspaceIndex].Live = workspaces[workspaceIndex].Live || session.Live
		}
	}
	return workspaces
}

func (s *Server) withLiveSessionFlags(sessions []Session) []Session {
	running := s.runningSessionIDs()
	for index := range sessions {
		if !running[sessions[index].ID] {
			continue
		}
		sessions[index].Live = true
		sessions[index].Active = true
		sessions[index].LastUsed = "live"
	}
	return sessions
}

func (s *Server) runningSessionIDs() map[string]bool {
	s.runner.mu.Lock()
	defer s.runner.mu.Unlock()
	running := make(map[string]bool, len(s.runner.running))
	for sessionID := range s.runner.running {
		running[sessionID] = true
	}
	return running
}

func (s *Server) deleteWorkspaceSessions(w http.ResponseWriter, r *http.Request) {
	deletedCount, err := s.store.DeleteWorkspaceSessions(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deletedCount": deletedCount, "sessions": []Session{}})
}
func (s *Server) workspaceFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.Files(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
func (s *Server) searchWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.SearchFiles(r.PathValue("workspaceID"), r.URL.Query().Get("q"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}
func (s *Server) workspaceCommands(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	commands := MockSlashCommands()
	var commandError string
	if s.config.EnablePiExecution {
		commands, err = ListPiCommands(r.Context(), root)
		if err != nil {
			commands = MockSlashCommands()
			commandError = err.Error()
		}
	}
	body := map[string]any{"commands": commands}
	if commandError != "" {
		body["error"] = commandError
	}
	writeJSON(w, http.StatusOK, body)
}
func (s *Server) workspaceModels(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	models := fallbackWorkspaceModels(root, nil)
	if s.config.EnablePiExecution {
		models, err = WorkspaceModels(r.Context(), root)
		if err != nil {
			models = fallbackWorkspaceModels(root, err)
		}
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) workspaceRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := MockRuntimeStatus()
	if s.config.EnablePiExecution {
		status, err = WorkspaceRuntimeStatus(r.Context(), root)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
func (s *Server) workspaceRuntimeModel(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := MockRuntimeModelStatus()
	if s.config.EnablePiExecution {
		status, err = WorkspaceRuntimeModelStatus(r.Context(), root)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
func (s *Server) workspaceRuntimeQuota(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := MockRuntimeQuotaStatus()
	if s.config.EnablePiExecution {
		status = WorkspaceRuntimeQuotaStatus(r.Context(), root, r.URL.Query().Get("model"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
func (s *Server) workspaceSettings(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	settings, err := WorkspaceSettings(root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}
func (s *Server) saveWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req SettingsPatchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	settings, err := SaveWorkspaceSettings(root, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}
func (s *Server) workspaceFileAsset(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	full, err := SafeJoin(root, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	full, err = filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, err)
		} else {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	if !pathWithinRoot(realRoot, full) {
		writeError(w, http.StatusForbidden, ErrWorkspaceForbidden)
		return
	}
	file, err := os.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, err)
		} else {
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
	w.Header().Set("Content-Type", detectPreviewMIME(full, probe[:n]))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) readWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	file, err := s.store.ReadFile(r.PathValue("workspaceID"), r.URL.Query().Get("path"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
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

type stagedMultipartUpload struct {
	path      string
	overwrite bool
	tempPath  string
	hasFile   bool
}

func (s *Server) uploadMultipartWorkspaceFile(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	staged, err := stageMultipartUpload(r, root, maxBytes)
	if err != nil {
		writeMultipartRequestError(w, err, maxBytes)
		return
	}
	defer os.Remove(staged.tempPath)
	if _, err := SafeJoin(root, staged.path); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, err := uploadStagedMultipartFile(root, staged, maxBytes)
	if err != nil {
		writeMultipartFileError(w, err, maxBytes)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func stageMultipartUpload(r *http.Request, root string, maxBytes int64) (stagedMultipartUpload, error) {
	temp, err := os.CreateTemp(root, ".pi-upload-*.tmp")
	if err != nil {
		return stagedMultipartUpload{}, err
	}
	staged := stagedMultipartUpload{tempPath: temp.Name()}
	keepTemp := false
	defer func() {
		_ = temp.Close()
		if !keepTemp {
			_ = os.Remove(temp.Name())
		}
	}()
	reader, err := r.MultipartReader()
	if err != nil {
		return stagedMultipartUpload{}, err
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stagedMultipartUpload{}, err
		}
		err = processMultipartPart(part, &staged, temp, maxBytes)
		closeErr := part.Close()
		if err != nil {
			return stagedMultipartUpload{}, err
		}
		if closeErr != nil {
			return stagedMultipartUpload{}, closeErr
		}
	}
	if !staged.hasFile {
		return stagedMultipartUpload{}, errors.New("file is required")
	}
	if err := temp.Close(); err != nil {
		return stagedMultipartUpload{}, err
	}
	keepTemp = true
	return staged, nil
}

func processMultipartPart(part *multipart.Part, staged *stagedMultipartUpload, temp *os.File, maxBytes int64) error {
	switch part.FormName() {
	case "path":
		value, err := readMultipartField(part)
		staged.path = value
		return err
	case "overwrite":
		value, err := readMultipartField(part)
		if err != nil {
			return err
		}
		if value != "" {
			staged.overwrite, err = strconv.ParseBool(value)
			if err != nil {
				return errors.New("overwrite must be a boolean")
			}
		}
		return nil
	case "file":
		if staged.hasFile {
			return errors.New("file must be provided once")
		}
		reader := io.Reader(part)
		if maxBytes > 0 {
			reader = io.LimitReader(part, maxBytes+1)
		}
		written, err := io.Copy(temp, reader)
		if err != nil {
			return err
		}
		if maxBytes > 0 && written > maxBytes {
			return &uploadTooLargeError{limit: maxBytes}
		}
		staged.hasFile = true
	}
	return nil
}

func uploadStagedMultipartFile(root string, staged stagedMultipartUpload, maxBytes int64) (FileContent, error) {
	file, err := os.Open(staged.tempPath)
	if err != nil {
		return FileContent{}, err
	}
	defer file.Close()
	return UploadWorkspaceFileStream(root, staged.path, file, maxBytes, staged.overwrite)
}

func writeMultipartRequestError(w http.ResponseWriter, err error, maxBytes int64) {
	var tooLarge *uploadTooLargeError
	if errors.As(err, &tooLarge) {
		writeUploadTooLarge(w, maxBytes)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}

func writeMultipartFileError(w http.ResponseWriter, err error, maxBytes int64) {
	var tooLarge *uploadTooLargeError
	if errors.As(err, &tooLarge) {
		writeUploadTooLarge(w, maxBytes)
		return
	}
	writeStoreError(w, err)
}

func readMultipartField(part *multipart.Part) (string, error) {
	const maxFieldBytes = 16 * 1024
	data, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxFieldBytes {
		return "", errors.New("multipart field is too large")
	}
	return string(data), nil
}
func (s *Server) gitStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.store.GitStatus(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}
func (s *Server) gitHistory(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	commits, err := RealGitHistory(r.Context(), root, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": commits})
}

func (s *Server) gitCommit(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	detail, err := RealGitCommitDetail(r.Context(), root, r.URL.Query().Get("hash"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) shellCommand(w http.ResponseWriter, r *http.Request) {
	var req ShellCommandRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := RunWorkspaceShellCommand(s.context(), s.store, r.PathValue("workspaceID"), req.Command)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
