package anchordtl

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type EventType string

const (
	EventOperatorRegistered EventType = "operator.registered"
	EventRouteOpened        EventType = "route.opened"
	EventRouteBound         EventType = "route.bound"
	EventRoutePaused        EventType = "route.paused"
	EventRouteClosed        EventType = "route.closed"
	EventGuaranteeDeposited EventType = "guarantee.deposited"
	EventGuaranteeReserved  EventType = "guarantee.reserved"
	EventGuaranteeReleased  EventType = "guarantee.released"
	EventObligationOpened   EventType = "obligation.opened"
	EventObligationSettled  EventType = "obligation.settled"
	EventSlashRecorded      EventType = "slash.recorded"
	EventReconciled         EventType = "reconcile.completed"
	EventLedgerPosted       EventType = "ledger.posted"
)

type Event struct {
	Type      EventType         `json:"type"`
	Epoch     Epoch             `json:"epoch"`
	Subject   string            `json:"subject"`
	Related   string            `json:"related,omitempty"`
	Time      time.Time         `json:"time"`
	Attribute map[string]string `json:"attribute,omitempty"`
}

func NewEvent(kind EventType, epoch Epoch, subject string) Event {
	return Event{
		Type:      kind,
		Epoch:     epoch,
		Subject:   subject,
		Time:      time.Now().UTC(),
		Attribute: make(map[string]string),
	}
}

func (e Event) With(key string, value string) Event {
	if e.Attribute == nil {
		e.Attribute = make(map[string]string)
	}
	e.Attribute[key] = value
	return e
}

func (e Event) WithRelated(related string) Event {
	e.Related = related
	return e
}

func (e Event) Summary() string {
	if e.Related == "" {
		return fmt.Sprintf("%s@%d:%s", e.Type, e.Epoch, e.Subject)
	}
	return fmt.Sprintf("%s@%d:%s->%s", e.Type, e.Epoch, e.Subject, e.Related)
}

type EventSink interface {
	Record(Event)
}

type MemoryEventLog struct {
	mu     sync.RWMutex
	events []Event
}

func NewMemoryEventLog() *MemoryEventLog {
	return &MemoryEventLog{events: make([]Event, 0, 128)}
}

func (l *MemoryEventLog) Record(event Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *MemoryEventLog) All() []Event {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := append([]Event(nil), l.events...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Epoch == out[j].Epoch {
			return out[i].Time.Before(out[j].Time)
		}
		return out[i].Epoch < out[j].Epoch
	})
	return out
}

func (l *MemoryEventLog) BySubject(subject string) []Event {
	all := l.All()
	out := make([]Event, 0)
	for _, event := range all {
		if event.Subject == subject || event.Related == subject {
			out = append(out, event)
		}
	}
	return out
}

func (l *MemoryEventLog) Count(kind EventType) int {
	all := l.All()
	count := 0
	for _, event := range all {
		if event.Type == kind {
			count++
		}
	}
	return count
}

func (l *MemoryEventLog) Last(kind EventType) (Event, bool) {
	all := l.All()
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Type == kind {
			return all[i], true
		}
	}
	return Event{}, false
}
