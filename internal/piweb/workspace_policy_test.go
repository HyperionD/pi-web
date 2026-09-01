package piweb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func policyForRoots(t *testing.T, roots ...string) WorkspacePolicy {
	t.Helper()
	values := make([]any, len(roots))
	for i, root := range roots {
		values[i] = root
	}
	policy, err := NewWorkspacePolicy(map[string]any{
		"workspace": map[string]any{"allowedRoots": values},
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestWorkspacePolicyUsesDirectoryBoundaries(t *testing.T) {
	root := t.TempDir()
	sibling := root + "-sibling"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := policyForRoots(t, root)
	if err := policy.CheckExistingDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckExistingDirectory(child); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckExistingDirectory(sibling); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("sibling error = %v", err)
	}
}

func TestWorkspacePolicyAllowsMissingOrEmptyRoots(t *testing.T) {
	root := t.TempDir()
	for name, config := range map[string]map[string]any{
		"missing": {},
		"empty":   {"workspace": map[string]any{"allowedRoots": []any{}}},
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := NewWorkspacePolicy(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := policy.CheckExistingDirectory(root); err != nil {
				t.Fatalf("root error = %v", err)
			}
		})
	}
}

func TestWorkspacePolicyAllowsMultipleRoots(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	outside := t.TempDir()
	policy := policyForRoots(t, first, second)
	if err := policy.CheckFolderPath(first); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckFolderPath(second); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckFolderPath(outside); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("outside error = %v", err)
	}
}

func TestWorkspacePolicyExpandsTildeAndResolvesRootSymlink(t *testing.T) {
	home := t.TempDir()
	realRoot := filepath.Join(home, "real")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("HOME", home)
	policy := policyForRoots(t, "~/link")
	if err := policy.CheckExistingDirectory(filepath.Join(realRoot, "..", "real")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacePolicyRejectsCandidateSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	policy := policyForRoots(t, root)
	if err := policy.CheckExistingDirectory(link); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestWorkspacePolicyValidatesCloneDestination(t *testing.T) {
	root := t.TempDir()
	policy := policyForRoots(t, root)
	want := filepath.Join(root, "clone")
	got, err := policy.ValidateCloneDestination(root, "clone")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}

	if err := os.Mkdir(filepath.Join(root, "existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ValidateCloneDestination(root, "existing"); err != nil {
		t.Fatalf("existing directory error = %v", err)
	}
}

func TestWorkspacePolicyRejectsCloneDestinationOutsideAllowedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	policy := policyForRoots(t, root)
	if _, err := policy.ValidateCloneDestination(outside, "clone"); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("outside parent error = %v", err)
	}
	for _, name := range []string{"", ".", "..", "nested/clone", `nested\\clone`} {
		if _, err := policy.ValidateCloneDestination(root, name); err == nil {
			t.Fatalf("name %q should be rejected", name)
		}
	}
}

func TestWorkspacePolicyInvalidFailsClosed(t *testing.T) {
	root := t.TempDir()
	cases := []map[string]any{
		{"workspace": "invalid"},
		{"workspace": map[string]any{"allowedRoots": []any{42}}},
		{"workspace": map[string]any{"allowedRoots": []any{filepath.Join(root, "missing")}}},
	}
	for _, config := range cases {
		policy, err := NewWorkspacePolicy(config)
		if !errors.Is(err, ErrWorkspacePolicyInvalid) {
			t.Fatalf("error = %v, want ErrWorkspacePolicyInvalid", err)
		}
		if err := policy.CheckExistingDirectory(root); !errors.Is(err, ErrWorkspacePolicyInvalid) {
			t.Fatalf("invalid policy check = %v", err)
		}
	}
}

func TestWorkspacePolicyHandlesDotDotBoundaries(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := root + "-outside"
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := policyForRoots(t, root)
	if err := policy.CheckExistingDirectory(filepath.Join(nested, ".", "..")); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckExistingDirectory(filepath.Join(root, "nested", "..", "..", filepath.Base(outside))); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("dot-dot escape error = %v", err)
	}
}

func TestLoadWorkspacePolicyUsesAppConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(t.TempDir(), "sessions"))
	if _, err := SaveAppConfig(map[string]any{
		"workspace": map[string]any{"allowedRoots": []any{root}},
	}); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadWorkspacePolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckExistingDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckExistingDirectory(t.TempDir()); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("outside error = %v", err)
	}
}
