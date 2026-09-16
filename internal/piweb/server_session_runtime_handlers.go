package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type sessionRuntimeCommandRequest struct {
	Command string `json:"command"`
	Args    string `json:"args,omitempty"`
}

func (s *Server) sessionCommands(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.config.EnablePiExecution {
		writeError(w, http.StatusNotImplemented, errors.New("pi execution is disabled"))
		return
	}
	runtime, err := s.ensureSessionRuntime(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	commands, err := ListRuntimeCommands(r.Context(), runtime)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": commands})
}

func (s *Server) sessionRuntimeCommand(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.config.EnablePiExecution {
		writeError(w, http.StatusNotImplemented, errors.New("pi execution is disabled"))
		return
	}
	var request sessionRuntimeCommandRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	command, err := allowlistedSessionRuntimeCommand(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	runtime, err := s.ensureSessionRuntime(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	response, err := runtime.Send(r.Context(), command)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if !response.Success {
		message := response.Error
		if message == "" {
			message = "pi runtime command failed"
		}
		writeError(w, http.StatusBadGateway, errors.New(message))
		return
	}
	writeRuntimeCommandData(w, response.Data)
}

func (s *Server) ensureSessionRuntime(ctx context.Context, sessionID string) (PiRuntime, error) {
	settings, err := s.modelSettingsForSession(sessionID)
	if err != nil {
		return nil, err
	}
	runtime, _, err := s.runtimeManager.EnsureOwnedWithInitializer(
		ctx, sessionID, 0, runtimeModelInitializer([]ModelStartupSettings{settings}),
	)
	return runtime, err
}

func allowlistedSessionRuntimeCommand(request sessionRuntimeCommandRequest) (RPCCommand, error) {
	name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(request.Command, "/")))
	if strings.TrimSpace(request.Args) != "" {
		return RPCCommand{}, errors.New("runtime command arguments are not supported")
	}
	switch name {
	case "reload":
		return RPCCommand{Type: "reload"}, nil
	case "session":
		return RPCCommand{Type: "get_session_stats"}, nil
	case "copy":
		return RPCCommand{Type: "get_last_assistant_text"}, nil
	case "clone":
		return RPCCommand{Type: "clone"}, nil
	default:
		return RPCCommand{}, errors.New("unsupported session runtime command")
	}
}

func writeRuntimeCommandData(w http.ResponseWriter, data json.RawMessage) {
	if len(data) == 0 || string(data) == "null" {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
