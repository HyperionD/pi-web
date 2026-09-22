package piweb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFileDiffReportsModifiedAddedAndDeletedFiles(t *testing.T) {
	if _, err := os.Stat(filepath.Join(".git")); err != nil {
		t.Skip("repository metadata is not available in this test environment")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "pi@example.test")
	runGit(t, root, "config", "user.name", "pi")
	for name, body := range map[string]string{"modified.txt": "before\n", "deleted.txt": "gone\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(root, "modified.txt"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{"modified.txt": "modified", "added.txt": "added", "deleted.txt": "deleted"}
	for path, wantStatus := range cases {
		result, err := GitFileDiff(root, path)
		if err != nil {
			t.Fatalf("GitFileDiff(%q): %v", path, err)
		}
		if !result.Supported || result.Status != wantStatus || !strings.Contains(result.Patch, "diff") {
			t.Fatalf("GitFileDiff(%q) = %#v, want supported %q patch", path, result, wantStatus)
		}
	}
}

func TestGitFileDiffRejectsTraversal(t *testing.T) {
	if _, err := GitFileDiff(t.TempDir(), "../secret"); err == nil {
		t.Fatal("GitFileDiff accepted a traversal path")
	}
}
