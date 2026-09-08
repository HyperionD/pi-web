package piweb

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
)

const maxJSONBodyBytes int64 = 1 << 20

var errRequestBodyTooLarge = errors.New("request body is too large")

type limitedJSONReader struct {
	reader   io.Reader
	read     int64
	maxBytes int64
}

func (r *limitedJSONReader) Read(p []byte) (int, error) {
	remaining := r.maxBytes + 1 - r.read
	if remaining <= 0 {
		return 0, errRequestBodyTooLarge
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func readJSON(r *http.Request, v any) error {
	return readJSONWithLimit(r, v, maxJSONBodyBytes)
}

func readJSONWithLimit(r *http.Request, v any, maxBytes int64) error {
	defer r.Body.Close()
	if maxBytes <= 0 {
		maxBytes = maxJSONBodyBytes
	}
	limited := &limitedJSONReader{reader: r.Body, maxBytes: maxBytes}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(v); err != nil {
		if limited.read > maxBytes {
			return errRequestBodyTooLarge
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if limited.read > maxBytes {
			return errRequestBodyTooLarge
		}
		if err == nil {
			return errors.New("request body must contain only one JSON value")
		}
		return err
	}
	if limited.read > maxBytes {
		return errRequestBodyTooLarge
	}
	return nil
}
func writeUploadTooLarge(w http.ResponseWriter, maxBytes int64) {
	writeError(w, http.StatusRequestEntityTooLarge, &uploadTooLargeError{limit: maxBytes})
}

func maxUploadJSONBodyBytes(maxBytes int64) int64 {
	return maxBytes + ((maxBytes+2)/3)*4 + 4096
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, err error) {
	if errors.Is(err, errRequestBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	writeJSON(w, status, ErrorResponse{Error: err.Error()})
}
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, ErrSafeUploadUnsupported) {
		writeError(w, http.StatusNotImplemented, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}

func writeWorkspaceSelectionError(w http.ResponseWriter, err error) {
	writeError(w, workspaceSelectionErrorStatus(err), err)
}

func workspaceSelectionErrorStatus(err error) int {
	if errors.Is(err, ErrWorkspaceForbidden) {
		return http.StatusForbidden
	}
	if errors.Is(err, ErrWorkspacePolicyInvalid) || isFilesystemError(err) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func isFilesystemError(err error) bool {
	var pathError *os.PathError
	return errors.As(err, &pathError) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrInvalid)
}
