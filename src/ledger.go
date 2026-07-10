package anchordtl

import (
	"sort"
	"time"
)

type LedgerEntryType string

const (
	LedgerDeposit    LedgerEntryType = "deposit"
	LedgerReserve    LedgerEntryType = "reserve"
	LedgerRelease    LedgerEntryType = "release"
	LedgerSettlement LedgerEntryType = "settlement"
	LedgerPenalty    LedgerEntryType = "penalty"
	LedgerAdjustment LedgerEntryType = "adjustment"
)

type LedgerEntry struct {
	Sequence int64           `json:"sequence"`
	Type     LedgerEntryType `json:"type"`
	Account  AccountID       `json:"account"`
	Amount   Amount          `json:"amount"`
	Epoch    Epoch           `json:"epoch"`
	Subject  string          `json:"subject"`
	Memo     string          `json:"memo"`
	Time     time.Time       `json:"time"`
}

type Ledger struct {
	entries  []LedgerEntry
	balances map[AccountID]Amount
	nextSeq  int64
	asset    string
}

func NewLedger(asset string) *Ledger {
	return &Ledger{
		entries:  make([]LedgerEntry, 0, 256),
		balances: make(map[AccountID]Amount),
		nextSeq:  1,
		asset:    NormalizeAsset(asset),
	}
}

func (l *Ledger) Post(kind LedgerEntryType, account AccountID, amount Amount, epoch Epoch, subject string, memo string) (LedgerEntry, error) {
	if amount.Asset != l.asset {
		return LedgerEntry{}, fail(CodeAssetMismatch, "ledger.post", "ledger asset mismatch")
	}
	if account == "" {
		return LedgerEntry{}, fail(CodeInvalid, "ledger.post", "account is required")
	}
	entry := LedgerEntry{
		Sequence: l.nextSeq,
		Type:     kind,
		Account:  account,
		Amount:   amount,
		Epoch:    epoch,
		Subject:  subject,
		Memo:     memo,
		Time:     time.Now().UTC(),
	}
	current, ok := l.balances[account]
	if !ok {
		current = ZeroAmount(l.asset)
	}
	next, err := current.Add(amount)
	if err != nil {
		return LedgerEntry{}, err
	}
	l.balances[account] = next
	l.entries = append(l.entries, entry)
	l.nextSeq++
	return entry, nil
}

func (l *Ledger) Debit(kind LedgerEntryType, account AccountID, amount Amount, epoch Epoch, subject string, memo string) (LedgerEntry, error) {
	if amount.Units < 0 {
		return LedgerEntry{}, fail(CodeInvalid, "ledger.debit", "debit amount must be positive")
	}
	return l.Post(kind, account, amount.Neg(), epoch, subject, memo)
}

func (l *Ledger) Transfer(kind LedgerEntryType, from AccountID, to AccountID, amount Amount, epoch Epoch, subject string, memo string) error {
	if _, err := l.Debit(kind, from, amount, epoch, subject, memo+":debit"); err != nil {
		return err
	}
	if _, err := l.Post(kind, to, amount, epoch, subject, memo+":credit"); err != nil {
		return err
	}
	return nil
}

func (l *Ledger) Balance(account AccountID) Amount {
	if amount, ok := l.balances[account]; ok {
		return amount
	}
	return ZeroAmount(l.asset)
}

func (l *Ledger) Entries() []LedgerEntry {
	out := append([]LedgerEntry(nil), l.entries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

func (l *Ledger) EntriesFor(account AccountID) []LedgerEntry {
	out := make([]LedgerEntry, 0)
	for _, entry := range l.entries {
		if entry.Account == account {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

func (l *Ledger) TrialBalance() (Amount, error) {
	total := ZeroAmount(l.asset)
	for _, balance := range l.balances {
		next, err := total.Add(balance)
		if err != nil {
			return Amount{}, err
		}
		total = next
	}
	return total, nil
}
