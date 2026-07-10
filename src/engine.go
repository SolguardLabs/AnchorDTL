package anchordtl

type Engine struct {
	Policy      RiskPolicy        `json:"policy"`
	Clock       EpochClock        `json:"clock"`
	Operators   *OperatorRegistry `json:"-"`
	Routes      *RouteBook        `json:"-"`
	Guarantees  *GuaranteeStore   `json:"-"`
	Obligations *ObligationBook   `json:"-"`
	Ledger      *Ledger           `json:"-"`
	Events      *MemoryEventLog   `json:"-"`
	Slasher     *SlashingService  `json:"-"`
	Reconciler  *Reconciler       `json:"-"`
}

func NewEngine(asset string) (*Engine, error) {
	policy := DefaultPolicy(asset)
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	events := NewMemoryEventLog()
	operators := NewOperatorRegistry()
	routes := NewRouteBook()
	guarantees := NewGuaranteeStore()
	obligations := NewObligationBook()
	engine := &Engine{
		Policy:      policy,
		Clock:       NewEpochClock(1),
		Operators:   operators,
		Routes:      routes,
		Guarantees:  guarantees,
		Obligations: obligations,
		Ledger:      NewLedger(policy.Asset),
		Events:      events,
	}
	engine.Slasher = NewSlashingService(policy, obligations, guarantees, routes, events)
	engine.Reconciler = NewReconciler(policy, routes, guarantees, obligations, events)
	return engine, nil
}

func MustNewEngine(asset string) *Engine {
	engine, err := NewEngine(asset)
	if err != nil {
		panic(err)
	}
	return engine
}

func (e *Engine) Now() Epoch {
	return e.Clock.Now()
}

func (e *Engine) Advance(delta Epoch) Epoch {
	return e.Clock.Advance(delta)
}

func (e *Engine) RegisterOperator(name string, controller string) (OperatorID, error) {
	profile := NewOperatorProfile(name, controller, e.Policy.Asset, e.Now())
	if err := e.Operators.Add(profile); err != nil {
		return "", err
	}
	e.Events.Record(NewEvent(EventOperatorRegistered, e.Now(), profile.ID.String()).With("name", name))
	return profile.ID, nil
}

func (e *Engine) OpenRoute(spec RouteSpec) (RouteID, error) {
	if err := e.Policy.ValidateRoute(spec); err != nil {
		return "", err
	}
	operator, err := e.Operators.RequireActive(spec.OperatorID)
	if err != nil {
		return "", err
	}
	route := NewRoute(spec, e.Now())
	if err := e.Routes.Add(route); err != nil {
		return "", err
	}
	operator.AddRoute(route.ID())
	e.Events.Record(NewEvent(EventRouteOpened, e.Now(), route.ID().String()).With("operator", operator.ID.String()))
	return route.ID(), nil
}

func (e *Engine) DepositGuarantee(operatorID OperatorID, label string, units int64) (GuaranteeID, error) {
	operator, err := e.Operators.RequireActive(operatorID)
	if err != nil {
		return "", err
	}
	deposit := NewAmount(e.Policy.Asset, units)
	account, err := NewGuaranteeAccount(operatorID, e.Policy.Asset, label, deposit, e.Now())
	if err != nil {
		return "", err
	}
	if err := e.Guarantees.Add(account); err != nil {
		return "", err
	}
	operator.AddGuarantee(account.ID)
	if _, err := e.Ledger.Post(LedgerDeposit, operator.Treasury, deposit, e.Now(), account.ID.String(), "guarantee deposit"); err != nil {
		return "", err
	}
	e.Events.Record(NewEvent(EventGuaranteeDeposited, e.Now(), account.ID.String()).With("amount", deposit.String()))
	return account.ID, nil
}

func (e *Engine) BindRouteGuarantee(routeID RouteID, guaranteeID GuaranteeID) error {
	route, err := e.Routes.Get(routeID)
	if err != nil {
		return err
	}
	guarantee, err := e.Guarantees.Get(guaranteeID)
	if err != nil {
		return err
	}
	if guarantee.OperatorID != route.OperatorID() {
		return fail(CodeConflict, "engine.bind", "guarantee operator does not own route")
	}
	if guarantee.Asset != route.Asset() {
		return fail(CodeAssetMismatch, "engine.bind", "guarantee asset does not match route")
	}
	if err := route.BindGuarantee(guaranteeID); err != nil {
		return err
	}
	e.Events.Record(NewEvent(EventRouteBound, e.Now(), routeID.String()).WithRelated(guaranteeID.String()))
	return nil
}

func (e *Engine) OpenObligation(routeID RouteID, reference string, principalUnits int64, close Epoch) (ObligationID, error) {
	route, err := e.Routes.Get(routeID)
	if err != nil {
		return "", err
	}
	if route.Status != RouteActive {
		return "", fail(CodeState, "engine.obligation", "route %s is not active", routeID)
	}
	if route.GuaranteeID == "" {
		return "", fail(CodeState, "engine.obligation", "route %s has no guarantee", routeID)
	}
	principal := NewAmount(route.Asset(), principalUnits)
	required, err := e.Policy.RequiredGuarantee(principal)
	if err != nil {
		return "", err
	}
	if required.LessThan(principal) {
		required = principal
	}
	terms, err := NewObligationTerms(reference, *route, e.Now(), close)
	if err != nil {
		return "", err
	}
	obligation, err := NewObligation(*route, route.GuaranteeID, principal, terms, e.Now())
	if err != nil {
		return "", err
	}
	guarantee, err := e.Guarantees.Get(route.GuaranteeID)
	if err != nil {
		return "", err
	}
	if err := guarantee.Reserve(routeID, obligation.ID, required, e.Now()); err != nil {
		return "", err
	}
	if err := e.Obligations.Add(obligation); err != nil {
		return "", err
	}
	if err := route.Metrics.AddPrincipal(principal); err != nil {
		return "", err
	}
	e.Events.Record(NewEvent(EventObligationOpened, e.Now(), obligation.ID.String()).
		WithRelated(routeID.String()).
		With("principal", principal.String()))
	return obligation.ID, nil
}

func (e *Engine) SettleObligation(obligationID ObligationID, units int64) error {
	obligation, err := e.Obligations.Get(obligationID)
	if err != nil {
		return err
	}
	amount := NewAmount(obligation.Principal.Asset, units)
	if err := obligation.MarkSettlement(amount, e.Now()); err != nil {
		return err
	}
	guarantee, err := e.Guarantees.Get(obligation.GuaranteeID)
	if err != nil {
		return err
	}
	if err := guarantee.Settle(obligation.RouteID, obligationID, amount, e.Now()); err != nil {
		return err
	}
	route, err := e.Routes.Get(obligation.RouteID)
	if err != nil {
		return err
	}
	if err := route.Metrics.AddSettlement(amount); err != nil {
		return err
	}
	if _, err := e.Ledger.Post(LedgerSettlement, obligation.Terms.Beneficiary, amount, e.Now(), obligationID.String(), "route settlement"); err != nil {
		return err
	}
	e.Events.Record(NewEvent(EventObligationSettled, e.Now(), obligationID.String()).With("amount", amount.String()))
	return nil
}

func (e *Engine) Slash(request SlashRequest) (SlashReceipt, error) {
	if request.Epoch == 0 {
		request.Epoch = e.Now()
	}
	receipt, err := e.Slasher.Slash(request)
	if err != nil {
		return SlashReceipt{}, err
	}
	for _, allocation := range receipt.Allocations {
		obligation, err := e.Obligations.Get(allocation.ObligationID)
		if err != nil {
			return SlashReceipt{}, err
		}
		if _, err := e.Ledger.Post(LedgerPenalty, obligation.Terms.Counterparty, allocation.Amount, request.Epoch, allocation.ObligationID.String(), "guarantee penalty"); err != nil {
			return SlashReceipt{}, err
		}
	}
	return receipt, nil
}

func (e *Engine) ReconcileRoute(routeID RouteID, closeRoute bool) (ReconcileResult, error) {
	return e.Reconciler.ReconcileRoute(routeID, e.Now(), closeRoute)
}

func (e *Engine) RouteReport(routeID RouteID) (RouteSolvencyReport, error) {
	return e.Reconciler.BuildRouteReport(routeID, e.Now())
}

func (e *Engine) EventsFor(subject string) []Event {
	return e.Events.BySubject(subject)
}

func (e *Engine) Validate() error {
	if err := e.Policy.Validate(); err != nil {
		return err
	}
	for _, guarantee := range e.Guarantees.List() {
		if err := guarantee.ValidateBasic(); err != nil {
			return err
		}
	}
	if _, err := e.Ledger.TrialBalance(); err != nil {
		return err
	}
	return nil
}
