package piweb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStateModelRPCLineUsesDisplayName(t *testing.T) {
	line := `{"id":"state","type":"response","command":"get_state","success":true,"data":{"model":{"id":"gpt-5.5","name":"GPT-5.5","provider":"openai-codex"},"thinkingLevel":"high"}}`
	model, matched, err := parseStateModelRPCLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || model != "GPT-5.5" {
		t.Fatalf("expected GPT-5.5, got matched=%v model=%q", matched, model)
	}
	status, matched, err := parseStateModelStatusRPCLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || status.ThinkingLevel != "high" {
		t.Fatalf("expected high thinking level, got matched=%v status=%+v", matched, status)
	}
}

func TestParseSessionStatsRPCLine(t *testing.T) {
	line := `{"id":"context","type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":60000,"contextWindow":200000,"percent":30}}}`
	usage, matched, err := parseSessionStatsRPCLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || usage == nil || usage.Tokens == nil || *usage.Tokens != 60000 || usage.ContextWindow != 200000 || usage.Percent == nil || *usage.Percent != 30 {
		t.Fatalf("unexpected context usage: matched=%v usage=%+v", matched, usage)
	}
	nullLine := `{"id":"context","type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":null,"contextWindow":200000,"percent":null}}}`
	usage, matched, err = parseSessionStatsRPCLine(nullLine)
	if err != nil || !matched || usage == nil || usage.Tokens != nil || usage.Percent != nil {
		t.Fatalf("expected nullable usage fields, matched=%v usage=%+v err=%v", matched, usage, err)
	}
}

func TestCurrentPiContextUsageQueriesSnapshotWithoutMutatingSession(t *testing.T) {
	binDir := t.TempDir()
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	original := []byte("{\"type\":\"session\",\"id\":\"s1\"}\n")
	if err := os.WriteFile(sessionFile, original, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
session=""
previous=""
for argument in "$@"; do
  if [ "$previous" = "--session" ]; then session="$argument"; fi
  previous="$argument"
done
[ -n "$session" ] || exit 3
[ "$session" != "$ORIGINAL_SESSION" ] || exit 4
cmp -s "$session" "$ORIGINAL_SESSION" || exit 5
printf '%s' "$session" > "$SNAPSHOT_PATH_RECORD"
echo '{"type":"model_change"}' >> "$session"
read command
case "$command" in
  *get_session_stats*) echo '{"id":"context","type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":64000,"contextWindow":128000,"percent":50}}}' ;;
  *) exit 6 ;;
esac
`
	piPath := filepath.Join(binDir, "pi")
	if err := os.WriteFile(piPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORIGINAL_SESSION", sessionFile)
	snapshotRecord := filepath.Join(t.TempDir(), "snapshot-path")
	t.Setenv("SNAPSHOT_PATH_RECORD", snapshotRecord)

	usage, err := CurrentPiContextUsage(context.Background(), sessionFile, t.TempDir())
	if err != nil || usage == nil || usage.Tokens == nil || *usage.Tokens != 64000 || usage.ContextWindow != 128000 {
		t.Fatalf("unexpected usage=%+v err=%v", usage, err)
	}
	if contents, err := os.ReadFile(sessionFile); err != nil || string(contents) != string(original) {
		t.Fatalf("context query mutated session: contents=%q err=%v", contents, err)
	}
	snapshotPath, err := os.ReadFile(snapshotRecord)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(snapshotPath))); !os.IsNotExist(err) {
		t.Fatalf("session snapshot was not removed: %v", err)
	}
}

func TestCurrentPiContextUsageRemovesSnapshotOnFailure(t *testing.T) {
	binDir := t.TempDir()
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte("{\"type\":\"session\",\"id\":\"s1\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotRecord := filepath.Join(t.TempDir(), "snapshot-path")
	script := `#!/bin/sh
previous=""
for argument in "$@"; do
  if [ "$previous" = "--session" ]; then session="$argument"; fi
  previous="$argument"
done
printf '%s' "$session" > "$SNAPSHOT_PATH_RECORD"
exit 7
`
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SNAPSHOT_PATH_RECORD", snapshotRecord)

	if _, err := CurrentPiContextUsage(context.Background(), sessionFile, t.TempDir()); err == nil {
		t.Fatal("expected context query failure")
	}
	snapshotPath, err := os.ReadFile(snapshotRecord)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(snapshotPath))); !os.IsNotExist(err) {
		t.Fatalf("failed context query left session snapshot: %v", err)
	}
}

func TestCreateSessionSnapshotDropsIncompleteTail(t *testing.T) {
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	complete := []byte("{\"type\":\"session\",\"id\":\"s1\"}\n")
	if err := os.WriteFile(sessionFile, append(complete, []byte(`{"type":"message"`)...), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotPath, cleanup, err := createSessionSnapshot(context.Background(), sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	contents, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(complete) {
		t.Fatalf("snapshot contains incomplete JSONL tail: %q", contents)
	}
}

func TestCreateSessionSnapshotDropsPartialSecondValue(t *testing.T) {
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	complete := []byte("{\"type\":\"session\",\"id\":\"s1\"}\n")
	if err := os.WriteFile(sessionFile, append(complete, []byte(`{} {`)...), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotPath, cleanup, err := createSessionSnapshot(context.Background(), sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot) != string(complete) {
		t.Fatalf("snapshot retained invalid final JSONL record: %q", snapshot)
	}
}

func TestCreateSessionSnapshotHonorsCancelledContextForEmptyFile(t *testing.T) {
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := createSessionSnapshot(ctx, sessionFile)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestCreateSessionSnapshotKeepsCompleteTailWithoutNewline(t *testing.T) {
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	contents := []byte("{\"type\":\"session\",\"id\":\"s1\"}\n{\"type\":\"model_change\"}")
	if err := os.WriteFile(sessionFile, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotPath, cleanup, err := createSessionSnapshot(context.Background(), sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot) != string(contents) {
		t.Fatalf("snapshot dropped complete final JSONL record: %q", snapshot)
	}
}

func TestCurrentPiSetModelSendsCommandAndParsesResponse(t *testing.T) {
	binDir := t.TempDir()
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(`{"type":"session","id":"s1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
found_provider=0
found_model=0
previous=""
for argument in "$@"; do
  if [ "$previous" = "--session" ] && [ "$argument" = "$EXPECTED_SESSION" ]; then found_session=1; fi
  previous="$argument"
done
[ "$found_session" = "1" ] || exit 3
read command
case "$command" in
  *set_model*) echo '{"id":"model","type":"response","command":"set_model","success":true,"data":{"provider":"openai-codex","id":"gpt-5.6-luna"}}' ;;
  *) exit 4 ;;
esac
`
	piPath := filepath.Join(binDir, "pi")
	if err := os.WriteFile(piPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EXPECTED_SESSION", sessionFile)

	if err := CurrentPiSetModel(context.Background(), sessionFile, t.TempDir(), "openai-codex", "gpt-5.6-luna"); err != nil {
		t.Fatalf("set model failed: %v", err)
	}
}

func TestQuotaMappersReturnRemainingPercent(t *testing.T) {
	primary := &quotaUsageWindow{UsedPercent: float64(16)}
	if got := remainingFromWindow(primary); got == nil || *got != 84 {
		t.Fatalf("expected codex remaining 84, got %v", got)
	}
	kimi := &kimiUsagePayload{Limits: []kimiLimit{{Label: "5h", UsedPercent: float64(80)}, {Label: "week", UsedPercent: float64(86)}}}
	if got := kimiWindow(kimi, "5H:"); got == nil || *got != 20 {
		t.Fatalf("expected kimi 5h remaining 20, got %v", got)
	}
	if got := kimiWindow(kimi, "7D:"); got == nil || *got != 14 {
		t.Fatalf("expected kimi weekly remaining 14, got %v", got)
	}
	zai := &zaiQuotaPayload{Limits: []zaiLimit{{Type: "TOKENS_LIMIT", Percentage: float64(10)}, {Type: "WEEKLY", Percentage: float64(90)}}}
	if got := zaiWindow(zai, "7D:"); got == nil || *got != 10 {
		t.Fatalf("expected zai weekly remaining 10, got %v", got)
	}
}

func TestWorkspaceRuntimeQuotaStatusFallsBackToEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_WEB_5H_QUOTA", "33")
	t.Setenv("PI_WEB_WEEKLY_QUOTA", "44")
	status := WorkspaceRuntimeQuotaStatus(context.Background(), root, "unknown")
	if status.FiveHourQuota == nil || *status.FiveHourQuota != 33 || status.WeeklyQuota == nil || *status.WeeklyQuota != 44 {
		t.Fatalf("unexpected quota status: %+v", status)
	}
}

func TestRuntimeQuotaIgnoresProjectFileAndClampsEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_WEB_5H_QUOTA", "120")
	t.Setenv("PI_WEB_WEEKLY_QUOTA", "14")
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pi", "web-status.json"), []byte(`{"fiveHourQuota":1,"weeklyQuota":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fiveHour, weekly := RuntimeQuota(root)
	if fiveHour == nil || *fiveHour != 100 {
		t.Fatalf("expected fiveHour 100, got %v", fiveHour)
	}
	if weekly == nil || *weekly != 14 {
		t.Fatalf("expected weekly 14, got %v", weekly)
	}
}

func TestWorkspaceRuntimeQuotaStatusPrefersLiveOverStaleProjectFile(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".pi", "agent", "auth.json"), []byte(`{"openai-codex":{"access":"oa","accountId":"acct"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pi", "web-status.json"), []byte(`{"fiveHourQuota":86,"weeklyQuota":54}`), 0o600); err != nil {
		t.Fatal(err)
	}
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"rate_limit":{"primary_window":{"used_percent":8},"secondary_window":{"used_percent":13}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	status := WorkspaceRuntimeQuotaStatus(context.Background(), root, "GPT-5.5")
	if status.FiveHourQuota == nil || *status.FiveHourQuota != 92 || status.WeeklyQuota == nil || *status.WeeklyQuota != 87 {
		t.Fatalf("expected live quota, got %+v", status)
	}
}
