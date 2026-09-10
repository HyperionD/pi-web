package piweb

import "context"

// Shutdown documents the store lifecycle. Store operations open SQLite handles per call,
// so there is no persistent handle to close today.
func (s *Store) Shutdown(context.Context) error { return nil }
