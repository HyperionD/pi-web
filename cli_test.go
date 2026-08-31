package main

import (
	"bytes"
	"io"
	"testing"
)

func TestRootCommandRunsServerWithDefaults(t *testing.T) {
	var got serverOptions
	cmd := newRootCommand(rootDependencies{
		stdout: io.Discard,
		stderr: io.Discard,
		serve: func(options serverOptions) error {
			got = options
			return nil
		},
	})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute root command: %v", err)
	}

	if got.Host != "0.0.0.0" || got.Port != "8732" || got.Mock {
		t.Fatalf("unexpected server options: %+v", got)
	}
}

func TestRootCommandRunsServerWithFlags(t *testing.T) {
	var got serverOptions
	cmd := newRootCommand(rootDependencies{
		stdout: io.Discard,
		stderr: io.Discard,
		serve: func(options serverOptions) error {
			got = options
			return nil
		},
	})
	cmd.SetArgs([]string{"--host", "0.0.0.0", "--port", "9999", "--mock"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute root command: %v", err)
	}

	if got.Host != "0.0.0.0" || got.Port != "9999" || !got.Mock {
		t.Fatalf("unexpected server options: %+v", got)
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	previousVersion := version
	version = "v1.5.4"
	t.Cleanup(func() { version = previousVersion })

	var out bytes.Buffer
	cmd := newRootCommand(rootDependencies{
		stdout: &out,
		stderr: io.Discard,
		serve: func(serverOptions) error {
			t.Fatal("server should not run")
			return nil
		},
	})
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}
	if got := out.String(); got != "pi-web v1.5.4\n" {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestUpdateCommandIsUnavailable(t *testing.T) {
	cmd := newRootCommand(rootDependencies{
		stdout: io.Discard,
		stderr: io.Discard,
		serve: func(serverOptions) error {
			t.Fatal("server should not run")
			return nil
		},
	})
	cmd.SetArgs([]string{"update"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected update command to be unavailable")
	}
}
