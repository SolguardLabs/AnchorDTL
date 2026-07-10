package anchordtl

import (
	"fmt"
	"time"
)

type Epoch uint64

type EpochClock struct {
	Current Epoch
}

func NewEpochClock(start Epoch) EpochClock {
	return EpochClock{Current: start}
}

func (c EpochClock) Now() Epoch {
	return c.Current
}

func (c *EpochClock) Advance(delta Epoch) Epoch {
	c.Current += delta
	return c.Current
}

func (c *EpochClock) Set(epoch Epoch) {
	c.Current = epoch
}

type SettlementWindow struct {
	Open  Epoch `json:"open"`
	Close Epoch `json:"close"`
}

func NewWindow(open Epoch, close Epoch) (SettlementWindow, error) {
	if close < open {
		return SettlementWindow{}, fail(CodeInvalid, "epoch.window", "close epoch precedes open epoch")
	}
	return SettlementWindow{Open: open, Close: close}, nil
}

func (w SettlementWindow) Contains(epoch Epoch) bool {
	return epoch >= w.Open && epoch <= w.Close
}

func (w SettlementWindow) Expired(epoch Epoch) bool {
	return epoch > w.Close
}

func (w SettlementWindow) String() string {
	return fmt.Sprintf("[%d,%d]", w.Open, w.Close)
}

type Schedule struct {
	Name       string           `json:"name"`
	Window     SettlementWindow `json:"window"`
	Grace      Epoch            `json:"grace"`
	RecordedAt time.Time        `json:"recorded_at"`
}

func NewSchedule(name string, open Epoch, close Epoch, grace Epoch) (Schedule, error) {
	window, err := NewWindow(open, close)
	if err != nil {
		return Schedule{}, err
	}
	return Schedule{
		Name:       name,
		Window:     window,
		Grace:      grace,
		RecordedAt: time.Now().UTC(),
	}, nil
}

func (s Schedule) Accepts(epoch Epoch) bool {
	return epoch >= s.Window.Open && epoch <= s.Window.Close+s.Grace
}

func (s Schedule) Final(epoch Epoch) bool {
	return epoch > s.Window.Close+s.Grace
}
