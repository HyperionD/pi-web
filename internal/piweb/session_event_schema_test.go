package piweb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenSQLiteSessionEventStoreCreatesSchemaAndDurabilityPragmas(t *testing.T) {
	store, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "pi-web.db"), NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(t.Context())
	for _, table := range []string{
		"session_deletions", "session_streams", "session_events", "session_metadata", "session_items", "session_item_chunks",
		"session_commands", "session_control_receipts", "session_run_manifests",
	} {
		if !sqliteTableExists(t, store.db, table) {
			t.Fatalf("missing table %s", table)
		}
	}
	if got := sqlitePragma(t, store.db, "synchronous"); got != "2" {
		t.Fatalf("synchronous = %s, want 2", got)
	}
	if got := sqlitePragma(t, store.db, "foreign_keys"); got != "1" {
		t.Fatalf("foreign_keys = %s, want 1", got)
	}
}

func TestOpenSQLiteSessionEventStoreAppliesPragmasToEveryPooledConnection(t *testing.T) {
	store, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "pi-web.db"), NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(t.Context())
	// A PRAGMA executed through db.Exec only configures one pooled connection.
	// Concurrent writers on the other connections would then fail immediately
	// with SQLITE_BUSY instead of waiting for the write lock.
	connections := make([]*sql.Conn, 0, 4)
	for index := 0; index < 4; index++ {
		conn, err := store.db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		connections = append(connections, conn)
	}
	for index, conn := range connections {
		var timeout int
		if err := conn.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 {
			t.Fatalf("connection %d busy_timeout = %d, want 5000", index, timeout)
		}
		var foreignKeys int
		if err := conn.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 {
			t.Fatalf("connection %d foreign_keys = %d, want 1", index, foreignKeys)
		}
	}
}

func TestSQLiteSessionEventStoreReopensAndCloseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-web.db")
	first, err := OpenSQLiteSessionEventStore(path, NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	second, err := OpenSQLiteSessionEventStore(path, NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(t.Context())
	if !sqliteTableExists(t, second.db, "piweb_schema_versions") {
		t.Fatal("schema version table missing after reopen")
	}
}

func TestSessionWakeHubCollapsesSignalsAndUnsubscribes(t *testing.T) {
	hub := NewSessionWakeHub()
	subscription := hub.Subscribe("s1")
	hub.Notify("s1")
	hub.Notify("s1")
	select {
	case <-subscription.Wake:
	case <-time.After(time.Second):
		t.Fatal("wake was not delivered")
	}
	select {
	case <-subscription.Wake:
		t.Fatal("duplicate wake was not collapsed")
	default:
	}
	subscription.Close()
	hub.Notify("s1")
	if err := hub.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func sqliteTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func sqlitePragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var value string
	if err := db.QueryRowContext(context.Background(), `PRAGMA `+name).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
