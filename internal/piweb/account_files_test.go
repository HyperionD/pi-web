package piweb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceFilesCannotFollowExternalSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("external-secret-needle"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWorkspaceFile(root, "secret.txt", 1024); err == nil {
		t.Error("read escaped")
	}
	if _, err := WriteWorkspaceFile(root, "secret.txt", "changed"); err == nil {
		t.Error("write escaped")
	}
	if _, err := CreateWorkspacePath(root, "escape/new.txt", "file", "changed"); err == nil {
		t.Error("create escaped")
	}
	if err := RenameWorkspacePath(root, "escape/secret.txt", "stolen.txt"); err == nil {
		t.Error("rename escaped")
	}
	if err := DeleteWorkspacePath(root, "escape/secret.txt"); err == nil {
		t.Error("delete escaped")
	}
	matches, err := SearchWorkspaceFiles(root, "external-secret-needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Error("search escaped")
	}
	data, err := os.ReadFile(secret)
	if err != nil || string(data) != "external-secret-needle" {
		t.Fatal("external file modified")
	}
}
