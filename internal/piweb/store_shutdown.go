package piweb

import "context"

// Shutdown documents the store lifecycle. Store operations open SQLite handles per call,
// so there is no persistent handle to close today.
func (s *Store) Shutdown(context.Context) error { return nil }

// DatabasePath exposes the configured pi-web database location so the server
// can attach the durable session event store. It is empty for mock stores.
func (s *Store) DatabasePath() string {
	if s == nil {
		return ""
	}
	return s.dbPath
}
