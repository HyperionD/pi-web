package piweb

// EventSubscription combines an atomic replay snapshot with its live stream.
// Close stops new delivery and cancels any blocked forwarder send; Events then closes.
// Close itself does not wait for a consumer, preventing shutdown deadlocks.
type EventSubscription struct {
	Events <-chan Event
	Replay []Event
	Close  func()
}
