package piweb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetSkillInvocationPreservesFrontmatterAndBody(t *testing.T) {
	original := []byte("\ufeff---\r\nname: demo\r\ndescription: Demo\r\ndisable-model-invocation: false\r\n---\r\n\r\nBody\r\n")
	updated, err := setSkillInvocation(original, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "\ufeff---\r\nname: demo\r\ndescription: Demo\r\ndisable-model-invocation: true\r\n---\r\n\r\nBody\r\n"
	if string(updated) != want {
		t.Fatalf("got %q, want %q", updated, want)
	}

	restored, err := setSkillInvocation(updated, true)
	if err != nil {
		t.Fatal(err)
	}
	want = "\ufeff---\r\nname: demo\r\ndescription: Demo\r\n---\r\n\r\nBody\r\n"
	if string(restored) != want {
		t.Fatalf("got %q after restore, want %q", restored, want)
	}
}

func TestSetSkillInvocationAddsAndRemovesKey(t *testing.T) {
	source := []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")
	disabled, err := setSkillInvocation(source, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(disabled) == string(source) {
		t.Fatal("expected disabling to add frontmatter")
	}
	enabled, err := setSkillInvocation(disabled, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(enabled) != string(source) {
		t.Fatalf("round trip changed content: %q", enabled)
	}
}

func TestSkillInvocationEnabledRecognizesValues(t *testing.T) {
	cases := []struct {
		name    string
		content string
		enabled bool
	}{
		{name: "missing", content: "---\nname: demo\n---\nBody\n", enabled: true},
		{name: "false", content: "---\ndisable-model-invocation: false\n---\n", enabled: true},
		{name: "true", content: "---\ndisable-model-invocation: true\n---\n", enabled: false},
		{name: "quoted", content: "---\n'disable-model-invocation': 'true'\n---\n", enabled: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			enabled, err := skillInvocationEnabled([]byte(test.content))
			if err != nil {
				t.Fatal(err)
			}
			if enabled != test.enabled {
				t.Fatalf("enabled=%v, want %v", enabled, test.enabled)
			}
		})
	}
}

func TestSetSkillInvocationRejectsUnsafeFrontmatter(t *testing.T) {
	cases := []string{
		"---\n{ disable-model-invocation: false }\n---\nBody\n",
		"---\ndisable-model-invocation: false\ndisable-model-invocation: true\n---\n",
		"---\ndisable-model-invocation: true\nBody\n",
	}
	for _, content := range cases {
		if _, err := setSkillInvocation([]byte(content), false); err == nil {
			t.Fatalf("expected unsafe frontmatter error for %q", content)
		}
	}
}

func TestWriteSkillFileAtomicPreservesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeSkillFileAtomic(path, []byte("after")); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "after" {
		t.Fatalf("content=%q, want after", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode=%o, want 640", got)
	}
}
