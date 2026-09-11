package piweb

import (
	"errors"
	"testing"
)

func TestAppendRollbackNeverLeavesPartialState(t *testing.T) {
	points := []EventStoreFaultPoint{
		FaultAfterEventInsert, FaultAfterProjectionWrite, FaultAfterStreamUpdate, FaultBeforeCommit,
	}
	for _, point := range points {
		t.Run(string(point), func(t *testing.T) {
			store := appendTestStore(t)
			receipt := appendTestRun(t, store, "req-1")
			store.eventStoreFaultHook = func(got EventStoreFaultPoint) error {
				if got == point {
					return errors.New("injected event store failure")
				}
				return nil
			}
			_, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1"))
			if err == nil {
				t.Fatal("expected injected failure")
			}
			var eventCount, itemCount int
			if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventToolStarted).Scan(&eventCount); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(`SELECT count(*) FROM session_items WHERE entity_id=?`, "tool:"+receipt.RunID+":call-1").Scan(&itemCount); err != nil {
				t.Fatal(err)
			}
			if eventCount != 0 || itemCount != 0 {
				t.Fatalf("partial state: events=%d items=%d", eventCount, itemCount)
			}
		})
	}
}

func TestAppendFailureDoesNotNotifyWakeHub(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	subscription := store.hub.Subscribe("s1")
	defer subscription.Close()
	store.eventStoreFaultHook = func(EventStoreFaultPoint) error { return errors.New("injected") }
	_, _ = store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1"))
	select {
	case <-subscription.Wake:
		t.Fatal("failed append notified subscribers")
	default:
	}
}
