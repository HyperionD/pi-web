package piweb

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSearchWorkspaceFilesMatchesPathAndTextContent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "needle-name-only.txt"), []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "content.txt"), []byte("needle inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "binary.txt"), []byte{0x00, 'n', 'e', 'e', 'd', 'l', 'e'}, 0o600); err != nil {
		t.Fatal(err)
	}
	matches, err := SearchWorkspaceFiles(root, "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0] != "src/content.txt" || matches[1] != "src/needle-name-only.txt" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestReadWorkspaceFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := ReadWorkspaceFile(root, "note.txt", 1024)
	if err != nil || content.Content != "hello" {
		t.Fatalf("unexpected content: %#v %v", content, err)
	}
	if _, err := ReadWorkspaceFile(root, "../secret", 1024); err == nil {
		t.Fatal("expected traversal to fail")
	}
}

func TestReadWorkspaceFilePreviewsTextLikeSourceFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"script.sh":       "#!/usr/bin/env bash\necho hello\n",
		"component.ts":    "export const answer: number = 42\n",
		"component.tsx":   "export function Demo() { return <div /> }\n",
		"README.md":       "# hello\n",
		"Dockerfile":      "FROM scratch\n",
		"config.yaml":     "name: demo\n",
		"unknown.configx": "plain text config\n",
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		content, err := ReadWorkspaceFile(root, name, 1024)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if content.PreviewKind != "text" || content.Content != body {
			t.Fatalf("%s: unexpected preview: %#v", name, content)
		}
	}
}

func TestReadWorkspaceFileDoesNotPreviewBinaryWithTextExtension(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "clip.txt")
	if err := os.WriteFile(path, []byte{0x00, 0x47, 0x40, 0x10, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := ReadWorkspaceFile(root, "clip.txt", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if content.PreviewKind == "text" || content.Content != "" {
		t.Fatalf("unexpected binary preview: %#v", content)
	}
}

func TestGitOriginalFileContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "pi@example.test")
	runGit(t, root, "config", "user.name", "pi")
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "src/main.txt")
	runGit(t, root, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(root, "src", "main.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	original, status := GitOriginalFileContent(root, "src/main.txt")
	if original != "base\n" || status != "tracked" {
		t.Fatalf("tracked original=%q status=%q", original, status)
	}
	original, status = GitOriginalFileContent(root, "src/new.txt")
	if original != "" || status != "untracked" {
		t.Fatalf("untracked original=%q status=%q", original, status)
	}
	content, err := ReadWorkspaceFile(root, "src/main.txt", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if content.Content != "changed\n" || content.OriginalContent != "base\n" || content.GitStatus != "tracked" {
		t.Fatalf("unexpected file content: %#v", content)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestRealFileTree(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodes, err := RealFileTree(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].Name != ".github" || nodes[1].Name != ".gitignore" || nodes[2].Name != "src" || len(nodes[2].Children) != 1 {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestWorkspaceFileMutations(t *testing.T) {
	root := t.TempDir()
	created, err := CreateWorkspacePath(root, "src/new.txt", "file", "hello")
	if err != nil || created.Content != "hello" {
		t.Fatalf("create file: %#v %v", created, err)
	}
	if _, err := CreateWorkspacePath(root, "src/new.txt", "file", "again"); err == nil {
		t.Fatal("expected duplicate create to fail")
	}
	if _, err := CreateWorkspacePath(root, "src/assets", "dir", ""); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := RenameWorkspacePath(root, "src/new.txt", "src/assets/renamed.txt"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := ReadWorkspaceFile(root, "src/assets/renamed.txt", 1024); err != nil {
		t.Fatalf("read renamed: %v", err)
	}
	if err := DeleteWorkspacePath(root, "src/assets/renamed.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ReadWorkspaceFile(root, "src/assets/renamed.txt", 1024); err == nil {
		t.Fatal("expected deleted file to be missing")
	}
}

func TestUploadWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	if _, err := UploadWorkspaceFile(root, "docs/image.png", []byte("png"), false); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := UploadWorkspaceFile(root, "docs/image.png", []byte("again"), false); err == nil {
		t.Fatal("expected upload without overwrite to fail")
	}
	file, err := UploadWorkspaceFile(root, "docs/image.png", []byte("again"), true)
	if err != nil {
		t.Fatalf("overwrite upload: %v", err)
	}
	if file.Path != "docs/image.png" {
		t.Fatalf("unexpected path: %#v", file)
	}
}

func TestUploadWorkspaceFileStreamRejectsParentSymlinkOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := UploadWorkspaceFileStream(root, "escape/payload.txt", strings.NewReader("secret"), 0, false); err == nil {
		t.Fatal("expected parent symlink to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "payload.txt")); !os.IsNotExist(err) {
		t.Fatalf("upload escaped workspace: %v", err)
	}
}

func TestUploadWorkspaceFileStreamNoOverwriteIsAtomicNoClobber(t *testing.T) {
	root := t.TempDir()
	arrived := make(chan struct{})
	release := make(chan struct{})
	results := make(chan struct {
		content string
		err     error
	}, 2)
	for _, content := range []string{"first", "second"} {
		go func(content string) {
			_, err := UploadWorkspaceFileStream(root, "same.txt", &barrierReader{
				data: content, arrived: arrived, release: release,
			}, 0, false)
			results <- struct {
				content string
				err     error
			}{content: content, err: err}
		}(content)
	}
	<-arrived
	<-arrived
	close(release)

	var success int
	var winner string
	for range 2 {
		result := <-results
		if result.err == nil {
			success++
			winner = result.content
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one no-clobber winner, got %d", success)
	}
	stored, err := os.ReadFile(filepath.Join(root, "same.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != winner {
		t.Fatalf("stored content=%q, winner=%q", stored, winner)
	}
	assertNoUploadTemps(t, root)
}

func TestUploadWorkspaceFileStreamPinsParentDuringSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	safe := filepath.Join(root, "safe")
	if err := os.Mkdir(safe, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "replacement")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	arrived := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := UploadWorkspaceFileStream(root, "safe/payload.txt", &barrierReader{
			data: "secret", arrived: arrived, release: release,
		}, 0, false)
		result <- err
	}()
	<-arrived
	if err := os.Rename(safe, filepath.Join(root, "safe-real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, safe); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-result

	if _, err := os.Stat(filepath.Join(outside, "payload.txt")); !os.IsNotExist(err) {
		t.Fatalf("upload escaped workspace: %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(root, "safe-real", "payload.txt"))
	if err != nil || string(stored) != "secret" {
		t.Fatalf("pinned parent content=%q err=%v", stored, err)
	}
}

type barrierReader struct {
	data    string
	arrived chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *barrierReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		r.arrived <- struct{}{}
		<-r.release
	})
	if r.data == "" {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestUploadWorkspaceFileStreamCleansTempWhenSourceFails(t *testing.T) {
	root := t.TempDir()
	_, err := UploadWorkspaceFileStream(root, "docs/fail.bin", failingUploadReader{}, 0, false)
	if err == nil || !strings.Contains(err.Error(), "source failed") {
		t.Fatalf("source error=%v", err)
	}
	assertNoUploadTemps(t, filepath.Join(root, "docs"))
}

type failingUploadReader struct{}

func (failingUploadReader) Read([]byte) (int, error) {
	return 0, errors.New("source failed")
}

func TestUploadWorkspaceFileStreamRejectsOversizeAndCleansTemp(t *testing.T) {
	root := t.TempDir()
	limit := int64(MinMaxUploadBytes)
	_, err := UploadWorkspaceFileStream(root, "docs/large.bin", bytes.NewReader(bytes.Repeat([]byte("x"), int(limit)+1)), limit, false)
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("oversize error=%v", err)
	}
	assertNoUploadTemps(t, filepath.Join(root, "docs"))
}

func assertNoUploadTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pi-upload-") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}
