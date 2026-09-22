package piweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

type watchedFileState struct {
	exists  bool
	version string
	size    int64
}

func (s *Server) workspaceFileWatch(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	rel := r.URL.Query().Get("path")
	if _, err := SafeJoin(root, rel); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	state, err := workspaceFileWatchState(root, rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			state = watchedFileState{}
		} else {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := writeFileWatchEvent(w, flusher, "connected", state); err != nil {
		return
	}

	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			next, err := workspaceFileWatchState(root, rel)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				next = watchedFileState{}
			}
			if next != state {
				state = next
				if err := writeFileWatchEvent(w, flusher, "change", state); err != nil {
					return
				}
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func workspaceFileWatchState(root, rel string) (watchedFileState, error) {
	file, err := openWorkspaceFileAsset(root, rel)
	if err != nil {
		return watchedFileState{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return watchedFileState{}, err
	}
	if info.IsDir() {
		return watchedFileState{}, errors.New("path is a directory")
	}
	return watchedFileState{exists: true, version: fileVersion(info), size: info.Size()}, nil
}

func writeFileWatchEvent(w http.ResponseWriter, flusher http.Flusher, event string, state watchedFileState) error {
	payload, err := json.Marshal(map[string]any{
		"exists":  state.exists,
		"size":    state.size,
		"version": state.version,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
