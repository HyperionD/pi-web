package piweb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type uploadTooLargeError struct {
	limit int64
}

func (e *uploadTooLargeError) Error() string {
	return fmt.Sprintf("file exceeds configured maximum of %d bytes", e.limit)
}

func UploadWorkspaceFile(root, rel string, data []byte, overwrite bool) (FileContent, error) {
	return UploadWorkspaceFileStream(root, rel, bytes.NewReader(data), 0, overwrite)
}

type uploadTransaction struct {
	parent         *os.File
	temp           *os.File
	tempName       string
	targetName     string
	removeTemp     func(*os.File, string) error
	committed      bool
	cleanupPending bool
	cleanupErr     error
}

func UploadWorkspaceFileStream(root, rel string, source io.Reader, maxBytes int64, overwrite bool) (FileContent, error) {
	tx, err := newUploadTransaction(root, rel, overwrite)
	if err != nil {
		return FileContent{}, err
	}
	defer tx.cleanup()
	if err := tx.copyFrom(source, maxBytes); err != nil {
		return FileContent{}, err
	}
	if err := tx.commit(overwrite); err != nil {
		return FileContent{}, err
	}
	return tx.readResult(root, rel)
}

func (tx *uploadTransaction) readResult(root, rel string) (FileContent, error) {
	if tx.temp == nil {
		return FileContent{}, errors.New("uploaded file handle is unavailable")
	}
	return readWorkspaceFileFromHandle(root, rel, filepath.Join(root, rel), tx.temp, 256*1024)
}

func (tx *uploadTransaction) copyFrom(source io.Reader, maxBytes int64) error {
	reader := source
	if maxBytes > 0 {
		reader = io.LimitReader(source, maxBytes+1)
	}
	written, err := io.Copy(tx.temp, reader)
	if err != nil {
		return err
	}
	if maxBytes > 0 && written > maxBytes {
		return &uploadTooLargeError{limit: maxBytes}
	}
	return nil
}

func (tx *uploadTransaction) commit(overwrite bool) error {
	if err := tx.temp.Sync(); err != nil {
		return err
	}
	if err := commitUploadTransaction(tx, overwrite); err != nil {
		if tx.committed {
			tx.cleanupPending = true
			return nil
		}
		return err
	}
	if !tx.cleanupPending {
		tx.tempName = ""
	}
	return nil
}

func (tx *uploadTransaction) cleanupTemp() error {
	if tx.tempName == "" || tx.parent == nil {
		return nil
	}
	remove := tx.removeTemp
	if remove == nil {
		remove = removeUploadTemp
	}
	var err error
	for range 3 {
		err = remove(tx.parent, tx.tempName)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			tx.cleanupPending = false
			tx.cleanupErr = nil
			tx.tempName = ""
			return nil
		}
	}
	tx.cleanupErr = err
	return err
}

func (tx *uploadTransaction) cleanup() {
	if tx.temp != nil {
		_ = tx.temp.Close()
		tx.temp = nil
	}
	if tx.tempName != "" && tx.parent != nil {
		if err := tx.cleanupTemp(); err != nil {
			tx.cleanupPending = true
		}
	}
	if tx.parent != nil {
		_ = tx.parent.Close()
		tx.parent = nil
	}
}
