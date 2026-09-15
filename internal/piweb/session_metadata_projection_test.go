package piweb

import (
	"encoding/json"
	"testing"
)

func TestSnapshotIncludesSessionMetadataProjection(t *testing.T) {
	store := appendTestStore(t)
	appendTestRun(t, store, "metadata-run")
	ordinal := int64(1)
	payload, _ := json.Marshal(map[string]any{"title": "New title"})
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", SourceID: "metadata:rename", SourceOrdinal: &ordinal,
		Type: EventSessionRenamed, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Metadata.Title != "New title" || snapshot.Metadata.UpdatedSeq != 3 {
		t.Fatalf("metadata = %#v", snapshot.Metadata)
	}
}
