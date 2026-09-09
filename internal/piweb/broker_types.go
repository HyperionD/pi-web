package piweb

// EventSubscription combines an atomic replay snapshot with its live stream.
// Close removes the subscriber and closes Events after any publish already in flight.
type EventSubscription struct {
	Events <-chan Event
	Replay []Event
	Close  func()
}
