package anchordtl

import "sort"

type GuaranteeStatus string

const (
	GuaranteeOpen      GuaranteeStatus = "open"
	GuaranteeFrozen    GuaranteeStatus = "frozen"
	GuaranteeExhausted GuaranteeStatus = "exhausted"
	GuaranteeClosed    GuaranteeStatus = "closed"
)

type CoverageSlot struct {
	ObligationID ObligationID `json:"obligation_id"`
	RouteID      RouteID      `json:"route_id"`
	Reserved     Amount       `json:"reserved"`
	Settled      Amount       `json:"settled"`
	Penalized    Amount       `json:"penalized"`
	Released     Amount       `json:"released"`
}

func (s CoverageSlot) Remaining() Amount {
	used := s.Settled.MustAdd(s.Penalized).MustAdd(s.Released)
	return s.Reserved.SubClamp(used)
}

func (s CoverageSlot) Empty() bool {
	return s.Remaining().IsZero()
}

type RouteExposure struct {
	RouteID     RouteID                        `json:"route_id"`
	Reserved    Amount                         `json:"reserved"`
	Settled     Amount                         `json:"settled"`
	Penalized   Amount                         `json:"penalized"`
	Released    Amount                         `json:"released"`
	LastUpdated Epoch                          `json:"last_updated"`
	Slots       map[ObligationID]*CoverageSlot `json:"slots"`
}

func NewRouteExposure(routeID RouteID, asset string, epoch Epoch) *RouteExposure {
	return &RouteExposure{
		RouteID:     routeID,
		Reserved:    ZeroAmount(asset),
		Settled:     ZeroAmount(asset),
		Penalized:   ZeroAmount(asset),
		Released:    ZeroAmount(asset),
		LastUpdated: epoch,
		Slots:       make(map[ObligationID]*CoverageSlot),
	}
}

func (e RouteExposure) Remaining() Amount {
	used := e.Settled.MustAdd(e.Penalized).MustAdd(e.Released)
	return e.Reserved.SubClamp(used)
}

func (e RouteExposure) UtilizationBps() int64 {
	if e.Reserved.Units == 0 {
		return 0
	}
	return ((e.Reserved.Units - e.Remaining().Units) * 10_000) / e.Reserved.Units
}

func (e RouteExposure) SlotIDs() []ObligationID {
	out := make([]ObligationID, 0, len(e.Slots))
	for id := range e.Slots {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type GuaranteeAccount struct {
	ID               GuaranteeID                    `json:"id"`
	OperatorID       OperatorID                     `json:"operator_id"`
	Asset            string                         `json:"asset"`
	Status           GuaranteeStatus                `json:"status"`
	Deposited        Amount                         `json:"deposited"`
	Active           Amount                         `json:"active"`
	Reserved         Amount                         `json:"reserved"`
	Slashed          Amount                         `json:"slashed"`
	Released         Amount                         `json:"released"`
	OpenedAt         Epoch                          `json:"opened_at"`
	ClosedAt         Epoch                          `json:"closed_at,omitempty"`
	Exposures        map[RouteID]*RouteExposure     `json:"exposures"`
	Slots            map[ObligationID]*CoverageSlot `json:"slots"`
	ObligationRoutes map[ObligationID]RouteID       `json:"obligation_routes"`
}

func NewGuaranteeAccount(operator OperatorID, asset string, label string, deposit Amount, epoch Epoch) (*GuaranteeAccount, error) {
	if err := deposit.Validate(); err != nil {
		return nil, err
	}
	if deposit.Asset != NormalizeAsset(asset) {
		return nil, fail(CodeAssetMismatch, "guarantee.new", "deposit asset does not match guarantee asset")
	}
	if !deposit.Positive() {
		return nil, fail(CodeInvalid, "guarantee.new", "deposit must be positive")
	}
	id := NewGuaranteeID(operator, asset, label)
	return &GuaranteeAccount{
		ID:               id,
		OperatorID:       operator,
		Asset:            NormalizeAsset(asset),
		Status:           GuaranteeOpen,
		Deposited:        deposit,
		Active:           deposit,
		Reserved:         ZeroAmount(asset),
		Slashed:          ZeroAmount(asset),
		Released:         ZeroAmount(asset),
		OpenedAt:         epoch,
		Exposures:        make(map[RouteID]*RouteExposure),
		Slots:            make(map[ObligationID]*CoverageSlot),
		ObligationRoutes: make(map[ObligationID]RouteID),
	}, nil
}

func (g *GuaranteeAccount) ensureAsset(op string, amount Amount) error {
	if amount.Asset != g.Asset {
		return fail(CodeAssetMismatch, op, "amount asset %s does not match guarantee asset %s", amount.Asset, g.Asset)
	}
	return nil
}

func (g *GuaranteeAccount) ensureOpen(op string) error {
	if g.Status != GuaranteeOpen {
		return fail(CodeState, op, "guarantee %s is %s", g.ID, g.Status)
	}
	return nil
}

func (g *GuaranteeAccount) exposure(routeID RouteID, epoch Epoch) *RouteExposure {
	exposure, ok := g.Exposures[routeID]
	if !ok {
		exposure = NewRouteExposure(routeID, g.Asset, epoch)
		g.Exposures[routeID] = exposure
	}
	return exposure
}

func (g *GuaranteeAccount) Free() Amount {
	return g.Active.SubClamp(g.Reserved)
}

func (g *GuaranteeAccount) Reserve(routeID RouteID, obligationID ObligationID, amount Amount, epoch Epoch) error {
	if err := g.ensureOpen("guarantee.reserve"); err != nil {
		return err
	}
	if err := g.ensureAsset("guarantee.reserve", amount); err != nil {
		return err
	}
	if !amount.Positive() {
		return fail(CodeInvalid, "guarantee.reserve", "reserve amount must be positive")
	}
	if _, exists := g.Slots[obligationID]; exists {
		return fail(CodeAlreadyExists, "guarantee.reserve", "obligation %s already reserved", obligationID)
	}
	if g.Free().LessThan(amount) {
		return fail(CodeInsufficient, "guarantee.reserve", "free guarantee %s below requested %s", g.Free(), amount)
	}
	exposure := g.exposure(routeID, epoch)
	slot := &CoverageSlot{
		ObligationID: obligationID,
		RouteID:      routeID,
		Reserved:     amount,
		Settled:      ZeroAmount(g.Asset),
		Penalized:    ZeroAmount(g.Asset),
		Released:     ZeroAmount(g.Asset),
	}
	exposure.Slots[obligationID] = slot
	g.Slots[obligationID] = slot
	g.ObligationRoutes[obligationID] = routeID
	if err := addAmount(&exposure.Reserved, amount); err != nil {
		return err
	}
	if err := addAmount(&g.Reserved, amount); err != nil {
		return err
	}
	exposure.LastUpdated = epoch
	return nil
}

func (g *GuaranteeAccount) Settle(routeID RouteID, obligationID ObligationID, amount Amount, epoch Epoch) error {
	if err := g.ensureAsset("guarantee.settle", amount); err != nil {
		return err
	}
	slot, ok := g.Slots[obligationID]
	if !ok {
		return fail(CodeNotFound, "guarantee.settle", "slot for obligation %s not found", obligationID)
	}
	if slot.RouteID != routeID {
		return fail(CodeConflict, "guarantee.settle", "obligation route does not match exposure route")
	}
	if slot.Remaining().LessThan(amount) {
		return fail(CodeInsufficient, "guarantee.settle", "settlement exceeds reserved slot")
	}
	exposure := g.exposure(routeID, epoch)
	if err := addAmount(&slot.Settled, amount); err != nil {
		return err
	}
	if err := addAmount(&exposure.Settled, amount); err != nil {
		return err
	}
	if err := subAmount(&g.Reserved, amount); err != nil {
		return err
	}
	exposure.LastUpdated = epoch
	return nil
}

func (g *GuaranteeAccount) PenalizeRoute(routeID RouteID, obligationID ObligationID, amount Amount, epoch Epoch) error {
	if err := g.ensureOpen("guarantee.penalize"); err != nil {
		return err
	}
	if err := g.ensureAsset("guarantee.penalize", amount); err != nil {
		return err
	}
	slot, ok := g.Slots[obligationID]
	if !ok {
		return fail(CodeNotFound, "guarantee.penalize", "slot for obligation %s not found", obligationID)
	}
	if slot.RouteID != routeID {
		return fail(CodeConflict, "guarantee.penalize", "obligation route does not match exposure route")
	}
	if slot.Remaining().LessThan(amount) {
		return fail(CodeInsufficient, "guarantee.penalize", "penalty exceeds reserved slot")
	}
	exposure := g.exposure(routeID, epoch)
	if err := addAmount(&slot.Penalized, amount); err != nil {
		return err
	}
	if err := addAmount(&exposure.Penalized, amount); err != nil {
		return err
	}
	if err := addAmount(&g.Slashed, amount); err != nil {
		return err
	}
	if err := subAmount(&g.Active, amount); err != nil {
		return err
	}
	if err := subAmount(&g.Reserved, amount.Min(g.Reserved)); err != nil {
		return err
	}
	if g.Active.IsZero() {
		g.Status = GuaranteeExhausted
	}
	exposure.LastUpdated = epoch
	return nil
}

func (g *GuaranteeAccount) ApplyPenaltyBatch(allocations map[ObligationID]Amount, primary RouteID, epoch Epoch) (Amount, error) {
	if err := g.ensureOpen("guarantee.batch"); err != nil {
		return Amount{}, err
	}
	total := ZeroAmount(g.Asset)
	ids := make([]ObligationID, 0, len(allocations))
	for id := range allocations {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		amount := allocations[id]
		if err := g.ensureAsset("guarantee.batch", amount); err != nil {
			return Amount{}, err
		}
		slot, ok := g.Slots[id]
		if !ok {
			return Amount{}, fail(CodeNotFound, "guarantee.batch", "slot for obligation %s not found", id)
		}
		if slot.Remaining().LessThan(amount) {
			return Amount{}, fail(CodeInsufficient, "guarantee.batch", "penalty exceeds reserved slot for %s", id)
		}
		if err := addAmount(&slot.Penalized, amount); err != nil {
			return Amount{}, err
		}
		next, err := total.Add(amount)
		if err != nil {
			return Amount{}, err
		}
		total = next
	}
	if total.IsZero() {
		return total, nil
	}
	if g.Active.LessThan(total) {
		return Amount{}, fail(CodeInsufficient, "guarantee.batch", "active guarantee below penalty")
	}
	exposure := g.exposure(primary, epoch)
	if err := addAmount(&exposure.Penalized, total); err != nil {
		return Amount{}, err
	}
	if err := addAmount(&g.Slashed, total); err != nil {
		return Amount{}, err
	}
	if err := subAmount(&g.Active, total); err != nil {
		return Amount{}, err
	}
	if err := subAmount(&g.Reserved, total.Min(g.Reserved)); err != nil {
		return Amount{}, err
	}
	exposure.LastUpdated = epoch
	if g.Active.IsZero() {
		g.Status = GuaranteeExhausted
	}
	return total, nil
}

func (g *GuaranteeAccount) ReleaseRoute(routeID RouteID, epoch Epoch) (Amount, error) {
	if err := g.ensureOpen("guarantee.release"); err != nil {
		return Amount{}, err
	}
	exposure, ok := g.Exposures[routeID]
	if !ok {
		return ZeroAmount(g.Asset), nil
	}
	remaining := exposure.Remaining()
	if remaining.IsZero() {
		return remaining, nil
	}
	if err := addAmount(&exposure.Released, remaining); err != nil {
		return Amount{}, err
	}
	if err := addAmount(&g.Released, remaining); err != nil {
		return Amount{}, err
	}
	if err := subAmount(&g.Reserved, remaining.Min(g.Reserved)); err != nil {
		return Amount{}, err
	}
	for _, slot := range exposure.Slots {
		slotRemaining := slot.Remaining()
		if slotRemaining.IsZero() {
			continue
		}
		_ = addAmount(&slot.Released, slotRemaining)
	}
	exposure.LastUpdated = epoch
	return remaining, nil
}

func (g *GuaranteeAccount) Close(epoch Epoch) error {
	if !g.Reserved.IsZero() {
		return fail(CodeState, "guarantee.close", "reserved guarantee remains")
	}
	g.Status = GuaranteeClosed
	g.ClosedAt = epoch
	return nil
}

func (g *GuaranteeAccount) RouteView(routeID RouteID) RouteExposure {
	exposure, ok := g.Exposures[routeID]
	if !ok {
		return *NewRouteExposure(routeID, g.Asset, g.OpenedAt)
	}
	cp := *exposure
	cp.Slots = make(map[ObligationID]*CoverageSlot, len(exposure.Slots))
	for id, slot := range exposure.Slots {
		slotCopy := *slot
		cp.Slots[id] = &slotCopy
	}
	return cp
}

func (g *GuaranteeAccount) SolventForRoute(routeID RouteID, required Amount) bool {
	if required.IsZero() {
		return true
	}
	if required.Asset != g.Asset {
		return false
	}
	view := g.RouteView(routeID)
	return view.Remaining().GreaterOrEqual(required)
}

func (g *GuaranteeAccount) ValidateBasic() error {
	if g.ID == "" || g.OperatorID == "" {
		return fail(CodeInvalid, "guarantee.validate", "identity fields are required")
	}
	if err := g.Deposited.Validate(); err != nil {
		return err
	}
	if err := g.Active.Validate(); err != nil {
		return err
	}
	if err := g.Reserved.Validate(); err != nil {
		return err
	}
	if g.Active.Units+g.Slashed.Units > g.Deposited.Units {
		return fail(CodeInvariant, "guarantee.validate", "active plus slashed exceeds deposited")
	}
	return nil
}

type GuaranteeStore struct {
	items map[GuaranteeID]*GuaranteeAccount
}

func NewGuaranteeStore() *GuaranteeStore {
	return &GuaranteeStore{items: make(map[GuaranteeID]*GuaranteeAccount)}
}

func (s *GuaranteeStore) Add(account *GuaranteeAccount) error {
	if account == nil {
		return fail(CodeInvalid, "guarantee.store", "account is nil")
	}
	if err := account.ValidateBasic(); err != nil {
		return err
	}
	if _, exists := s.items[account.ID]; exists {
		return fail(CodeAlreadyExists, "guarantee.store", "guarantee %s already exists", account.ID)
	}
	s.items[account.ID] = account
	return nil
}

func (s *GuaranteeStore) Get(id GuaranteeID) (*GuaranteeAccount, error) {
	if s == nil {
		return nil, fail(CodeNotFound, "guarantee.get", "store is nil")
	}
	account, ok := s.items[id]
	if !ok {
		return nil, fail(CodeNotFound, "guarantee.get", "guarantee %s not found", id)
	}
	return account, nil
}

func (s *GuaranteeStore) ByOperator(operator OperatorID) []GuaranteeAccount {
	out := make([]GuaranteeAccount, 0)
	for _, item := range s.items {
		if item.OperatorID == operator {
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *GuaranteeStore) List() []GuaranteeAccount {
	out := make([]GuaranteeAccount, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func addAmount(target *Amount, amount Amount) error {
	next, err := target.Add(amount)
	if err != nil {
		return err
	}
	*target = next
	return nil
}

func subAmount(target *Amount, amount Amount) error {
	next, err := target.Sub(amount)
	if err != nil {
		return err
	}
	*target = next
	return nil
}
