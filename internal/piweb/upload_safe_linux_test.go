//go:build linux

package piweb

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpenVerifiedUploadRootRejectsDifferentDirectoryIdentity(t *testing.T) {
	expected := t.TempDir()
	other := t.TempDir()
	opened, err := os.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var expectedIdentity unix.Stat_t
	if err := unix.Stat(expected, &expectedIdentity); err != nil {
		t.Fatal(err)
	}
	if err := validateUploadRootIdentity(opened, expectedIdentity); err == nil {
		t.Fatal("expected a device/inode mismatch")
	}
}
