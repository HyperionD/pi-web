package piweb

const sessionEventSchemaVersion = 3

const sessionEventSchemaSQL = `
CREATE TABLE IF NOT EXISTS piweb_schema_versions (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_deletions (
  session_id TEXT PRIMARY KEY,
  deleted_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_streams (
  session_id TEXT PRIMARY KEY,
  current_seq INTEGER NOT NULL DEFAULT 0 CHECK(current_seq >= 0),
  min_retained_seq INTEGER NOT NULL DEFAULT 1 CHECK(min_retained_seq >= 1),
  projection_schema_version INTEGER NOT NULL DEFAULT 1,
  projection_generation INTEGER NOT NULL DEFAULT 1 CHECK(projection_generation > 0),
  status TEXT NOT NULL DEFAULT 'idle',
  current_run_id TEXT,
  current_run_source_ordinal INTEGER NOT NULL DEFAULT 0,
  current_run_source_hash TEXT,
  current_run_source_seq INTEGER,
  next_ordinal INTEGER NOT NULL DEFAULT 1,
  source_path TEXT,
  source_size INTEGER,
  source_mtime_ns INTEGER,
  source_content_hash TEXT,
  updated_at TEXT NOT NULL,
  CHECK(min_retained_seq <= current_seq + 1)
);
CREATE TABLE IF NOT EXISTS session_events (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  seq INTEGER NOT NULL CHECK(seq > 0),
  run_id TEXT,
  source_id TEXT NOT NULL,
  source_ordinal INTEGER NOT NULL DEFAULT 0,
  event_type TEXT NOT NULL,
  entity_id TEXT,
  payload_hash TEXT NOT NULL DEFAULT '',
  payload_json BLOB NOT NULL,
  occurred_at TEXT NOT NULL,
  PRIMARY KEY(session_id, seq),
  UNIQUE(session_id, source_id),
  UNIQUE(session_id, run_id, source_ordinal)
);
CREATE INDEX IF NOT EXISTS idx_session_events_time ON session_events(session_id, occurred_at);
CREATE TABLE IF NOT EXISTS session_control_receipts (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  source_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  committed_seq INTEGER NOT NULL,
  PRIMARY KEY(session_id, source_id)
);
CREATE TABLE IF NOT EXISTS session_run_manifests (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  run_id TEXT NOT NULL,
  manifest_hash TEXT NOT NULL,
  manifest_json BLOB NOT NULL,
  completed_seq INTEGER NOT NULL,
  PRIMARY KEY(session_id, run_id)
);
CREATE TABLE IF NOT EXISTS session_metadata (
  session_id TEXT PRIMARY KEY REFERENCES session_streams(session_id) ON DELETE CASCADE,
  payload_json BLOB NOT NULL,
  updated_seq INTEGER NOT NULL,
  FOREIGN KEY(session_id) REFERENCES session_streams(session_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS session_items (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  item_id TEXT NOT NULL,
  ordinal INTEGER NOT NULL,
  kind TEXT NOT NULL,
  origin TEXT NOT NULL,
  run_id TEXT,
  entity_id TEXT,
  payload_json BLOB NOT NULL,
  created_seq INTEGER NOT NULL,
  updated_seq INTEGER NOT NULL,
  PRIMARY KEY(session_id, item_id),
  UNIQUE(session_id, ordinal)
);
CREATE INDEX IF NOT EXISTS idx_session_items_page ON session_items(session_id, ordinal DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_items_entity ON session_items(session_id, entity_id) WHERE entity_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS session_item_chunks (
  session_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  content_version INTEGER NOT NULL,
  chunk_index INTEGER NOT NULL,
  content BLOB NOT NULL,
  PRIMARY KEY(session_id, item_id, content_version, chunk_index),
  FOREIGN KEY(session_id, item_id) REFERENCES session_items(session_id, item_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS session_commands (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  client_request_id TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  run_id TEXT NOT NULL,
  accepted_seq INTEGER NOT NULL,
  status TEXT NOT NULL,
  response_json BLOB NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(session_id, client_request_id)
);
`
