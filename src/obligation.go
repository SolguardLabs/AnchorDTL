package anchordtl

import "sort"

type ObligationState string

const (
	ObligationOpen       ObligationState = "open"
	ObligationFunded     ObligationState = "funded"
	ObligationSettled    ObligationState = "settled"
	ObligationDefaulted  ObligationState = "defaulted"
	ObligationSlashed    ObligationState = "slashed"
	ObligationReconciled ObligationState = "reconciled"
	ObligationClosed     ObligationState = "closed"
)

type ObligationTerms struct {
	Reference    string           `json:"reference"`
	Window       SettlementWindow `json:"window"`
	ServiceLevel string           `json:"service_level"`
	Counterparty AccountID        `json:"counterparty"`
	Beneficiary  AccountID        `json:"beneficiary"`
	MetadataHash string           `json:"metadata_hash"`
}

func NewObligationTerms(ref string, route Route, open Epoch, close Epoch) (ObligationTerms, error) {
	window, err := NewWindow(open, close)
	if err != nil {
		return ObligationTerms{}, err
	}
	return ObligationTerms{
		Reference:    ref,
		Window:       window,
		ServiceLevel: "standard",
		Counterparty: NewAccountID("counterparty-"+route.Spec.Source, route.Asset()),
		Beneficiary:  NewAccountID("beneficiary-"+route.Spec.Destination, route.Asset()),
		MetadataHash: deriveSortedID("terms", ref, route.Spec.Source, route.Spec.Destination, route.Asset()),
	}, nil
}

type Obligation struct {
	ID          ObligationID    `json:"id"`
	OperatorID  OperatorID      `json:"operator_id"`
	RouteID     RouteID         `json:"route_id"`
	GuaranteeID GuaranteeID     `json:"guarantee_id"`
	State       ObligationState `json:"state"`
	Principal   Amount          `json:"principal"`
	Settled     Amount          `json:"settled"`
	Penalty     Amount          `json:"penalty"`
	OpenedAt    Epoch           `json:"opened_at"`
	UpdatedAt   Epoch           `json:"updated_at"`
	Terms       ObligationTerms `json:"terms"`
}

func NewObligation(route Route, guarantee GuaranteeID, principal Amount, terms ObligationTerms, epoch Epoch) (Obligation, error) {
	if err := principal.Validate(); err != nil {
		return Obligation{}, err
	}
	if principal.Asset != route.Asset() {
		return Obligation{}, fail(CodeAssetMismatch, "obligation.new", "principal asset does not match route")
	}
	if route.GuaranteeID != guarantee {
		return Obligation{}, fail(CodeConflict, "obligation.new", "route is not bound to guarantee")
	}
	if !principal.Positive() {
		return Obligation{}, fail(CodeInvalid, "obligation.new", "principal must be positive")
	}
	id := NewObligationID(route.ID(), terms.Reference)
	return Obligation{
		ID:          id,
		OperatorID:  route.OperatorID(),
		RouteID:     route.ID(),
		GuaranteeID: guarantee,
		State:       ObligationFunded,
		Principal:   principal,
		Settled:     ZeroAmount(principal.Asset),
		Penalty:     ZeroAmount(principal.Asset),
		OpenedAt:    epoch,
		UpdatedAt:   epoch,
		Terms:       terms,
	}, nil
}

func (o Obligation) Outstanding() Amount {
	used := o.Settled.MustAdd(o.Penalty)
	return o.Principal.SubClamp(used)
}

func (o Obligation) Due(epoch Epoch) bool {
	return o.Terms.Window.Expired(epoch)
}

func (o Obligation) Final(epoch Epoch, challenge Epoch) bool {
	return epoch > o.Terms.Window.Close+challenge
}

func (o Obligation) IsTerminal() bool {
	switch o.State {
	case ObligationSettled, ObligationReconciled, ObligationClosed:
		return true
	default:
		return false
	}
}

func (o *Obligation) MarkSettlement(amount Amount, epoch Epoch) error {
	if amount.Asset != o.Principal.Asset {
		return fail(CodeAssetMismatch, "obligation.settle", "settlement asset mismatch")
	}
	if !amount.Positive() {
		return fail(CodeInvalid, "obligation.settle", "settlement must be positive")
	}
	if o.Outstanding().LessThan(amount) {
		return fail(CodeInsufficient, "obligation.settle", "settlement exceeds outstanding amount")
	}
	if err := addAmount(&o.Settled, amount); err != nil {
		return err
	}
	o.UpdatedAt = epoch
	if o.Outstanding().IsZero() {
		o.State = ObligationSettled
	}
	return nil
}

func (o *Obligation) MarkPenalty(amount Amount, epoch Epoch) error {
	if amount.Asset != o.Principal.Asset {
		return fail(CodeAssetMismatch, "obligation.penalty", "penalty asset mismatch")
	}
	if amount.IsZero() {
		return nil
	}
	if o.Outstanding().LessThan(amount) {
		return fail(CodeInsufficient, "obligation.penalty", "penalty exceeds outstanding amount")
	}
	if err := addAmount(&o.Penalty, amount); err != nil {
		return err
	}
	o.UpdatedAt = epoch
	if o.Outstanding().IsZero() {
		o.State = ObligationSlashed
	} else {
		o.State = ObligationDefaulted
	}
	return nil
}

func (o *Obligation) MarkReconciled(epoch Epoch) {
	if o.State != ObligationClosed {
		o.State = ObligationReconciled
	}
	o.UpdatedAt = epoch
}

func (o *Obligation) Close(epoch Epoch) error {
	if !o.Outstanding().IsZero() {
		return fail(CodeState, "obligation.close", "obligation %s still has outstanding amount", o.ID)
	}
	o.State = ObligationClosed
	o.UpdatedAt = epoch
	return nil
}

type ObligationBook struct {
	items map[ObligationID]*Obligation
}

func NewObligationBook() *ObligationBook {
	return &ObligationBook{items: make(map[ObligationID]*Obligation)}
}

func (b *ObligationBook) Add(obligation Obligation) error {
	if _, exists := b.items[obligation.ID]; exists {
		return fail(CodeAlreadyExists, "obligation.add", "obligation %s already exists", obligation.ID)
	}
	cp := obligation
	b.items[obligation.ID] = &cp
	return nil
}

func (b *ObligationBook) Get(id ObligationID) (*Obligation, error) {
	if b == nil {
		return nil, fail(CodeNotFound, "obligation.get", "book is nil")
	}
	obligation, ok := b.items[id]
	if !ok {
		return nil, fail(CodeNotFound, "obligation.get", "obligation %s not found", id)
	}
	return obligation, nil
}

func (b *ObligationBook) ByRoute(routeID RouteID) []Obligation {
	out := make([]Obligation, 0)
	for _, obligation := range b.items {
		if obligation.RouteID == routeID {
			out = append(out, *obligation)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (b *ObligationBook) ByGuarantee(guaranteeID GuaranteeID) []Obligation {
	out := make([]Obligation, 0)
	for _, obligation := range b.items {
		if obligation.GuaranteeID == guaranteeID {
			out = append(out, *obligation)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (b *ObligationBook) OutstandingByRoute(routeID RouteID, asset string) (Amount, error) {
	total := ZeroAmount(asset)
	for _, obligation := range b.items {
		if obligation.RouteID != routeID {
			continue
		}
		if obligation.State == ObligationClosed || obligation.State == ObligationReconciled {
			continue
		}
		next, err := total.Add(obligation.Outstanding())
		if err != nil {
			return Amount{}, err
		}
		total = next
	}
	return total, nil
}

func (b *ObligationBook) List() []Obligation {
	out := make([]Obligation, 0, len(b.items))
	for _, obligation := range b.items {
		out = append(out, *obligation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (b *ObligationBook) OpenIDs(ids []ObligationID) ([]*Obligation, error) {
	out := make([]*Obligation, 0, len(ids))
	for _, id := range ids {
		obligation, err := b.Get(id)
		if err != nil {
			return nil, err
		}
		if obligation.IsTerminal() {
			return nil, fail(CodeState, "obligation.openids", "obligation %s is terminal", id)
		}
		out = append(out, obligation)
	}
	return out, nil
}
