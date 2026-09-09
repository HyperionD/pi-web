package piweb

type EventSubscription struct {
	Events <-chan Event
	Replay []Event
	Close  func()
}
