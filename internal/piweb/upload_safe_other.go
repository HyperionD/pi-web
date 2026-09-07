//go:build !linux

package piweb

import "os"

func newUploadTransaction(root, rel string, overwrite bool) (*uploadTransaction, error) {
	return nil, ErrSafeUploadUnsupported
}

func commitUploadTransaction(tx *uploadTransaction, overwrite bool) error {
	return ErrSafeUploadUnsupported
}

func removeUploadTemp(parent *os.File, name string) error {
	return ErrSafeUploadUnsupported
}
