package anchordtl

import "sort"

type RouteStatus string

const (
	RouteDraft    RouteStatus = "draft"
	RouteActive   RouteStatus = "active"
	RoutePaused   RouteStatus = "paused"
	RouteDraining RouteStatus = "draining"
	RouteClosed   RouteStatus = "closed"
)

type RouteSpec struct {
	ID          RouteID    `json:"id"`
	OperatorID  OperatorID `json:"operator_id"`
	Source      string     `json:"source"`
	Destination string     `json:"destination"`
	Asset       string     `json:"asset"`
	Capacity    Amount     `json:"capacity"`
	FeeBps      int64      `json:"fee_bps"`
	Label       string     `json:"label"`
}

func NewRouteSpec(operator OperatorID, source string, destination string, asset string, capacity int64, feeBps int64) RouteSpec {
	lane := JoinID(source, destination, asset)
	return RouteSpec{
		ID:          NewRouteID(operator, lane),
		OperatorID:  operator,
		Source:      source,
		Destination: destination,
		Asset:       NormalizeAsset(asset),
		Capacity:    NewAmount(asset, capacity),
		FeeBps:      feeBps,
		Label:       lane,
	}
}

func (s RouteSpec) Validate() error {
	if s.ID == "" {
		return fail(CodeInvalid, "route.validate", "id is required")
	}
	if s.OperatorID == "" {
		return fail(CodeInvalid, "route.validate", "operator is required")
	}
	if s.Source == "" || s.Destination == "" {
		return fail(CodeInvalid, "route.validate", "source and destination are required")
	}
	if s.Source == s.Destination {
		return fail(CodeInvalid, "route.validate", "source and destination must differ")
	}
	if err := s.Capacity.Validate(); err != nil {
		return err
	}
	if s.Asset != s.Capacity.Asset {
		return fail(CodeAssetMismatch, "route.validate", "asset mismatch in route capacity")
	}
	return nil
}

type RouteMetrics struct {
	OpenedObligations  int    `json:"opened_obligations"`
	SettledObligations int    `json:"settled_obligations"`
	TotalPrincipal     Amount `json:"total_principal"`
	TotalSettled       Amount `json:"total_settled"`
	TotalPenalty       Amount `json:"total_penalty"`
}

func NewRouteMetrics(asset string) RouteMetrics {
	return RouteMetrics{
		TotalPrincipal: ZeroAmount(asset),
		TotalSettled:   ZeroAmount(asset),
		TotalPenalty:   ZeroAmount(asset),
	}
}

type Route struct {
	Spec        RouteSpec    `json:"spec"`
	Status      RouteStatus  `json:"status"`
	GuaranteeID GuaranteeID  `json:"guarantee_id,omitempty"`
	OpenedAt    Epoch        `json:"opened_at"`
	ClosedAt    Epoch        `json:"closed_at,omitempty"`
	Metrics     RouteMetrics `json:"metrics"`
}

func NewRoute(spec RouteSpec, epoch Epoch) Route {
	return Route{
		Spec:     spec,
		Status:   RouteActive,
		OpenedAt: epoch,
		Metrics:  NewRouteMetrics(spec.Asset),
	}
}

func (r Route) ID() RouteID {
	return r.Spec.ID
}

func (r Route) OperatorID() OperatorID {
	return r.Spec.OperatorID
}

func (r Route) Asset() string {
	return r.Spec.Asset
}

func (r Route) IsOpen() bool {
	return r.Status == RouteActive || r.Status == RoutePaused || r.Status == RouteDraining
}

func (r *Route) BindGuarantee(id GuaranteeID) error {
	if r.Status == RouteClosed {
		return fail(CodeState, "route.bind", "route %s is closed", r.ID())
	}
	r.GuaranteeID = id
	return nil
}

func (r *Route) Pause() error {
	if r.Status != RouteActive {
		return fail(CodeState, "route.pause", "route %s cannot pause from %s", r.ID(), r.Status)
	}
	r.Status = RoutePaused
	return nil
}

func (r *Route) Resume() error {
	if r.Status != RoutePaused {
		return fail(CodeState, "route.resume", "route %s cannot resume from %s", r.ID(), r.Status)
	}
	r.Status = RouteActive
	return nil
}

func (r *Route) Drain() error {
	if r.Status != RouteActive && r.Status != RoutePaused {
		return fail(CodeState, "route.drain", "route %s cannot drain from %s", r.ID(), r.Status)
	}
	r.Status = RouteDraining
	return nil
}

func (r *Route) Close(epoch Epoch) error {
	if r.Status == RouteClosed {
		return fail(CodeState, "route.close", "route %s already closed", r.ID())
	}
	r.Status = RouteClosed
	r.ClosedAt = epoch
	return nil
}

func (m *RouteMetrics) AddPrincipal(amount Amount) error {
	next, err := m.TotalPrincipal.Add(amount)
	if err != nil {
		return err
	}
	m.TotalPrincipal = next
	m.OpenedObligations++
	return nil
}

func (m *RouteMetrics) AddSettlement(amount Amount) error {
	next, err := m.TotalSettled.Add(amount)
	if err != nil {
		return err
	}
	m.TotalSettled = next
	m.SettledObligations++
	return nil
}

func (m *RouteMetrics) AddPenalty(amount Amount) error {
	next, err := m.TotalPenalty.Add(amount)
	if err != nil {
		return err
	}
	m.TotalPenalty = next
	return nil
}

type RouteBook struct {
	items map[RouteID]*Route
}

func NewRouteBook() *RouteBook {
	return &RouteBook{items: make(map[RouteID]*Route)}
}

func (b *RouteBook) Add(route Route) error {
	if err := route.Spec.Validate(); err != nil {
		return err
	}
	if _, exists := b.items[route.ID()]; exists {
		return fail(CodeAlreadyExists, "route.add", "route %s already exists", route.ID())
	}
	cp := route
	b.items[route.ID()] = &cp
	return nil
}

func (b *RouteBook) Get(id RouteID) (*Route, error) {
	if b == nil {
		return nil, fail(CodeNotFound, "route.get", "route book is nil")
	}
	route, ok := b.items[id]
	if !ok {
		return nil, fail(CodeNotFound, "route.get", "route %s not found", id)
	}
	return route, nil
}

func (b *RouteBook) ByOperator(id OperatorID) []Route {
	out := make([]Route, 0)
	for _, route := range b.items {
		if route.OperatorID() == id {
			out = append(out, *route)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

func (b *RouteBook) List() []Route {
	out := make([]Route, 0, len(b.items))
	for _, route := range b.items {
		out = append(out, *route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
