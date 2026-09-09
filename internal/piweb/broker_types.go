package piweb

// EventSubscription combines an atomic replay snapshot with its live stream.
// Close stops new delivery, then Events closes after queued and in-flight events drain.
// Close itself does not wait for a blocked consumer, preventing shutdown deadlocks.
type EventSubscription struct {
	Events <-chan Event
	Replay []Event
	Close  func()
}
