//go:build linux

package piweb

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const uploadDirectoryFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW

func newUploadTransaction(root, rel string, overwrite bool) (*uploadTransaction, error) {
	full, err := SafeJoin(root, rel)
	if err != nil {
		return nil, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	rootDir, err := openUploadDirectoryPath(realRoot)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(filepath.Clean(root), full)
	if err != nil {
		_ = rootDir.Close()
		return nil, err
	}
	parent, target, err := openUploadParent(rootDir, filepath.Dir(relative), filepath.Base(relative))
	if err != nil {
		return nil, err
	}
	if err := checkUploadTargetAt(parent, target, overwrite); err != nil {
		_ = parent.Close()
		return nil, err
	}
	temp, tempName, err := createUploadTemp(parent)
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	return &uploadTransaction{
		parent: parent, temp: temp, tempName: tempName, targetName: target,
	}, nil
}

func openUploadDirectoryPath(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	currentFD, err := unix.Open(string(filepath.Separator), uploadDirectoryFlags, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(currentFD), string(filepath.Separator))
	for _, part := range splitUploadPath(strings.TrimPrefix(absolute, string(filepath.Separator))) {
		childFD, err := unix.Openat(int(current.Fd()), part, uploadDirectoryFlags, 0)
		if err != nil {
			_ = current.Close()
			return nil, err
		}
		child := os.NewFile(uintptr(childFD), part)
		_ = current.Close()
		current = child
	}
	return current, nil
}

func openUploadParent(root *os.File, parentRel, target string) (*os.File, string, error) {
	current := root
	parts := splitUploadPath(parentRel)
	for _, part := range parts {
		childFD, err := unix.Openat(int(current.Fd()), part, uploadDirectoryFlags, 0)
		if errors.Is(err, unix.ENOENT) {
			if err := unix.Mkdirat(int(current.Fd()), part, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
				_ = current.Close()
				return nil, "", err
			}
			childFD, err = unix.Openat(int(current.Fd()), part, uploadDirectoryFlags, 0)
		}
		if err != nil {
			_ = current.Close()
			return nil, "", err
		}
		child := os.NewFile(uintptr(childFD), part)
		_ = current.Close()
		current = child
	}
	return current, target, nil
}

func splitUploadPath(path string) []string {
	clean := filepath.Clean(path)
	if clean == "." || clean == string(filepath.Separator) {
		return nil
	}
	return strings.Split(clean, string(filepath.Separator))
}

func checkUploadTargetAt(parent *os.File, target string, overwrite bool) error {
	var info unix.Stat_t
	err := unix.Fstatat(int(parent.Fd()), target, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT == unix.S_IFDIR {
		return errors.New("path is a directory")
	}
	if !overwrite {
		return errors.New("path already exists")
	}
	return nil
}

func createUploadTemp(parent *os.File) (*os.File, string, error) {
	var random [12]byte
	for range 10 {
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", err
		}
		name := fmt.Sprintf(".pi-upload-%x.tmp", random)
		fd, err := unix.Openat(int(parent.Fd()), name,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return os.NewFile(uintptr(fd), name), name, nil
	}
	return nil, "", errors.New("could not create upload temporary file")
}

func commitUploadTransaction(tx *uploadTransaction, overwrite bool) error {
	parentFD := int(tx.parent.Fd())
	if overwrite {
		return unix.Renameat(parentFD, tx.tempName, parentFD, tx.targetName)
	}
	if err := unix.Linkat(parentFD, tx.tempName, parentFD, tx.targetName, 0); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errors.New("path already exists")
		}
		return err
	}
	if err := unix.Unlinkat(parentFD, tx.tempName, 0); err != nil {
		return err
	}
	return nil
}

func removeUploadTemp(parent *os.File, name string) error {
	return unix.Unlinkat(int(parent.Fd()), name, 0)
}
