//go:build !linux

package piweb

import (
	"errors"
	"os"
)

func newUploadTransaction(root, rel string, overwrite bool) (*uploadTransaction, error) {
	return nil, errors.New("safe upload operations are unavailable on this platform")
}

func commitUploadTransaction(tx *uploadTransaction, overwrite bool) error {
	return errors.New("safe upload operations are unavailable on this platform")
}

func removeUploadTemp(parent *os.File, name string) error {
	return errors.New("safe upload operations are unavailable on this platform")
}
