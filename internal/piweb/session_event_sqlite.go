package piweb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func OpenSQLiteSessionEventStore(path string, hub *SessionWakeHub) (*SQLiteSessionEventStore, error) {
	if path == "" {
		return nil, errors.New("event store database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create event store directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open event store database: %w", err)
	}
	store := &SQLiteSessionEventStore{db: db, hub: hub}
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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA wal_autocheckpoint=1000;`); err != nil {
		return fmt.Errorf("configure event store database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
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

func (s *SQLiteSessionEventStore) withImmediateTx(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	s.closeMu.Lock()
	closed := s.closed
	s.closeMu.Unlock()
	if closed {
		return sql.ErrConnDone
	}
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
