package piweb

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
)

const maxMultipartFieldBytes = 16 * 1024
const maxMultipartParts = 3

func (s *Server) uploadMultipartWorkspaceFile(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	tx, path, overwrite, err := parseMultipartUpload(r, root, maxBytes)
	if err != nil {
		writeMultipartRequestError(w, err, maxBytes)
		return
	}
	defer tx.cleanup()
	if err := tx.commit(overwrite); err != nil {
		writeMultipartFileError(w, err, maxBytes)
		return
	}
	file, err := tx.readResult(root, path)
	if err != nil {
		writeMultipartFileError(w, err, maxBytes)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func parseMultipartUpload(r *http.Request, root string, maxBytes int64) (*uploadTransaction, string, bool, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, "", false, err
	}
	metadata := multipartMetadata{}
	var tx *uploadTransaction
	partCount := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		partCount++
		if partCount > maxMultipartParts {
			if tx != nil {
				tx.cleanup()
			}
			return nil, "", false, errors.New("too many multipart parts")
		}
		if err != nil {
			if tx != nil {
				tx.cleanup()
			}
			return nil, "", false, err
		}
		tx, err = processMultipartPart(part, &metadata, tx, root, maxBytes)
		if err != nil {
			if tx != nil {
				tx.cleanup()
			}
			return nil, "", false, err
		}
	}
	if tx == nil {
		return nil, "", false, errors.New("file is required")
	}
	return tx, metadata.path, metadata.overwrite, nil
}

type multipartMetadata struct {
	path          string
	overwrite     bool
	pathSeen      bool
	overwriteSeen bool
	fileSeen      bool
}

func processMultipartPart(part *multipart.Part, metadata *multipartMetadata, tx *uploadTransaction, root string, maxBytes int64) (*uploadTransaction, error) {
	switch part.FormName() {
	case "path":
		return tx, processMultipartPath(part, metadata, root)
	case "overwrite":
		return tx, processMultipartOverwrite(part, metadata)
	case "file":
		return processMultipartFile(part, metadata, tx, root, maxBytes)
	default:
		return tx, errors.New("unknown multipart field")
	}
}

func processMultipartPath(part *multipart.Part, metadata *multipartMetadata, root string) error {
	if metadata.fileSeen {
		return errors.New("path and overwrite must precede file")
	}
	value, err := readMultipartField(part)
	if err != nil {
		return err
	}
	if metadata.pathSeen {
		return errors.New("path must be provided once")
	}
	if _, err := SafeJoin(root, value); err != nil {
		return err
	}
	metadata.path, metadata.pathSeen = value, true
	return nil
}

func processMultipartOverwrite(part *multipart.Part, metadata *multipartMetadata) error {
	if metadata.fileSeen {
		return errors.New("path and overwrite must precede file")
	}
	value, err := readMultipartField(part)
	if err != nil {
		return err
	}
	if metadata.overwriteSeen {
		return errors.New("overwrite must be provided once")
	}
	metadata.overwrite, err = strconv.ParseBool(value)
	if err != nil {
		return errors.New("overwrite must be a boolean")
	}
	metadata.overwriteSeen = true
	return nil
}

func processMultipartFile(part *multipart.Part, metadata *multipartMetadata, tx *uploadTransaction, root string, maxBytes int64) (*uploadTransaction, error) {
	if metadata.fileSeen {
		return tx, errors.New("file must be provided once")
	}
	if !metadata.pathSeen || !metadata.overwriteSeen {
		return nil, errors.New("path and overwrite must precede file")
	}
	tx, err := newUploadTransaction(root, metadata.path, metadata.overwrite)
	if err != nil {
		return nil, err
	}
	if err := tx.copyFrom(part, maxBytes); err != nil {
		tx.cleanup()
		return nil, err
	}
	metadata.fileSeen = true
	return tx, nil
}

func writeMultipartRequestError(w http.ResponseWriter, err error, maxBytes int64) {
	var tooLarge *uploadTooLargeError
	if errors.As(err, &tooLarge) {
		writeUploadTooLarge(w, maxBytes)
		return
	}
	if errors.Is(err, ErrSafeUploadUnsupported) {
		writeError(w, http.StatusNotImplemented, err)
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
	data, err := io.ReadAll(io.LimitReader(part, maxMultipartFieldBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxMultipartFieldBytes {
		return "", errors.New("multipart field is too large")
	}
	return string(data), nil
}
