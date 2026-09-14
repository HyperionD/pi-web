package piweb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
)

func OpenSQLiteSessionEventStore(path string, hub *SessionWakeHub) (*SQLiteSessionEventStore, error) {
	if path == "" {
		return nil, errors.New("event store database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create event store directory: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path, eventStorePragmas...))
	if err != nil {
		return nil, fmt.Errorf("open event store database: %w", err)
	}
	store := &SQLiteSessionEventStore{db: db, hub: hub, writeSlot: make(chan struct{}, 1)}
	if err := configureEventDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateSessionEventSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func configureEventDB(db *sql.DB) error {
	// Pragmas come from the DSN so that every pooled connection receives them.
	// Verify the connection the pool hands us actually carries busy_timeout,
	// otherwise concurrent writers would fail with SQLITE_BUSY instead of
	// waiting for the write lock.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	var timeout int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		return fmt.Errorf("configure event store database: %w", err)
	}
	if timeout <= 0 {
		return errors.New("configure event store database: busy_timeout was not applied")
	}
	return nil
}

func migrateSessionEventSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin event store migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(sessionEventSchemaSQL); err != nil {
		return fmt.Errorf("create event store schema: %w", err)
	}
	var current int
	err = tx.QueryRow(`SELECT coalesce(max(version), 0) FROM piweb_schema_versions`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read event store schema version: %w", err)
	}
	if current > sessionEventSchemaVersion {
		return fmt.Errorf("unsupported event store schema version %d", current)
	}
	if current < sessionEventSchemaVersion {
		if _, err := tx.Exec(`INSERT INTO piweb_schema_versions(version, applied_at) VALUES (?, ?)`, sessionEventSchemaVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record event store schema version: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event store migration: %w", err)
	}
	return nil
}

const (
	eventStoreBusyRetries   = 8
	eventStoreBusyBaseDelay = 20 * time.Millisecond
	eventStoreBusyMaxDelay  = 2 * time.Second
)

// withImmediateTx serializes writers in-process and retries transient lock
// contention. SQLite allows a single writer, so without this queue every
// concurrent session turned normal contention into SQLITE_BUSY, which then
// poisoned the run and left it without a terminal event.
func (s *SQLiteSessionEventStore) withImmediateTx(ctx context.Context, fn func(*sql.Conn) error) error {
	if err := s.acquireWrite(ctx); err != nil {
		return err
	}
	defer s.releaseWrite()
	delay := eventStoreBusyBaseDelay
	var err error
	for attempt := 0; ; attempt++ {
		err = s.runImmediateTx(ctx, fn)
		if err == nil || !isSQLiteBusyError(err) || attempt >= eventStoreBusyRetries {
			return err
		}
		if waitErr := sleepContext(ctx, delay); waitErr != nil {
			return err
		}
		if delay < eventStoreBusyMaxDelay {
			delay *= 2
			if delay > eventStoreBusyMaxDelay {
				delay = eventStoreBusyMaxDelay
			}
		}
	}
}

func (s *SQLiteSessionEventStore) acquireWrite(ctx context.Context) error {
	s.closeMu.Lock()
	closed := s.closed
	s.closeMu.Unlock()
	if closed {
		return sql.ErrConnDone
	}
	select {
	case s.writeSlot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *SQLiteSessionEventStore) releaseWrite() {
	<-s.writeSlot
}

func (s *SQLiteSessionEventStore) runImmediateTx(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err = fn(conn); err != nil {
		return err
	}
	if err = s.injectFault(FaultBeforeCommit); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}

// isSQLiteBusyError reports whether err is a transient write-lock failure that
// is worth retrying. modernc.org/sqlite returns *sqlite.Error for real lock
// contention; the string fallback keeps the retry testable and covers wrapped
// driver errors.
func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() & 0xff {
		case sqliteBusyCode, sqliteLockedCode:
			return true
		default:
			return false
		}
	}
	message := err.Error()
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

const (
	sqliteBusyCode   = 5
	sqliteLockedCode = 6
)

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *SQLiteSessionEventStore) Close(ctx context.Context) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	s.closeMu.Unlock()
	if s.hub != nil {
		if err := s.hub.Shutdown(ctx); err != nil {
			return err
		}
	}
	_, checkpointErr := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	closeErr := s.db.Close()
	return errors.Join(checkpointErr, closeErr)
}

var _ SessionEventStore = (*SQLiteSessionEventStore)(nil)
