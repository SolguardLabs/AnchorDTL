package anchordtl

import "sort"

type OperatorStatus string

const (
	OperatorPending   OperatorStatus = "pending"
	OperatorActive    OperatorStatus = "active"
	OperatorSuspended OperatorStatus = "suspended"
	OperatorRetired   OperatorStatus = "retired"
)

type OperatorProfile struct {
	ID           OperatorID     `json:"id"`
	Name         string         `json:"name"`
	Controller   AccountID      `json:"controller"`
	Treasury     AccountID      `json:"treasury"`
	Status       OperatorStatus `json:"status"`
	Tier         RiskTier       `json:"tier"`
	Routes       []RouteID      `json:"routes"`
	Guarantees   []GuaranteeID  `json:"guarantees"`
	RegisteredAt Epoch          `json:"registered_at"`
}

func NewOperatorProfile(name string, controller string, asset string, epoch Epoch) OperatorProfile {
	id := NewOperatorID(name)
	return OperatorProfile{
		ID:           id,
		Name:         name,
		Controller:   NewAccountID(controller, asset),
		Treasury:     NewAccountID(name+"-treasury", asset),
		Status:       OperatorActive,
		Tier:         RiskTierStandard,
		Routes:       make([]RouteID, 0),
		Guarantees:   make([]GuaranteeID, 0),
		RegisteredAt: epoch,
	}
}

func (p *OperatorProfile) AddRoute(id RouteID) {
	if p == nil {
		return
	}
	for _, existing := range p.Routes {
		if existing == id {
			return
		}
	}
	p.Routes = append(p.Routes, id)
	sort.Slice(p.Routes, func(i, j int) bool { return p.Routes[i] < p.Routes[j] })
}

func (p *OperatorProfile) AddGuarantee(id GuaranteeID) {
	if p == nil {
		return
	}
	for _, existing := range p.Guarantees {
		if existing == id {
			return
		}
	}
	p.Guarantees = append(p.Guarantees, id)
	sort.Slice(p.Guarantees, func(i, j int) bool { return p.Guarantees[i] < p.Guarantees[j] })
}

func (p OperatorProfile) Active() bool {
	return p.Status == OperatorActive
}

func (p OperatorProfile) Validate() error {
	if p.ID == "" {
		return fail(CodeInvalid, "operator.validate", "id is required")
	}
	if p.Name == "" {
		return fail(CodeInvalid, "operator.validate", "name is required")
	}
	if p.Controller == "" {
		return fail(CodeInvalid, "operator.validate", "controller is required")
	}
	return nil
}

type OperatorRegistry struct {
	items map[OperatorID]*OperatorProfile
}

func NewOperatorRegistry() *OperatorRegistry {
	return &OperatorRegistry{items: make(map[OperatorID]*OperatorProfile)}
}

func (r *OperatorRegistry) Add(profile OperatorProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	if _, exists := r.items[profile.ID]; exists {
		return fail(CodeAlreadyExists, "operator.add", "operator %s already exists", profile.ID)
	}
	cp := profile
	r.items[profile.ID] = &cp
	return nil
}

func (r *OperatorRegistry) Get(id OperatorID) (*OperatorProfile, error) {
	if r == nil {
		return nil, fail(CodeNotFound, "operator.get", "registry is nil")
	}
	op, ok := r.items[id]
	if !ok {
		return nil, fail(CodeNotFound, "operator.get", "operator %s not found", id)
	}
	return op, nil
}

func (r *OperatorRegistry) RequireActive(id OperatorID) (*OperatorProfile, error) {
	op, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	if !op.Active() {
		return nil, fail(CodeState, "operator.active", "operator %s is %s", id, op.Status)
	}
	return op, nil
}

func (r *OperatorRegistry) SetStatus(id OperatorID, status OperatorStatus) error {
	op, err := r.Get(id)
	if err != nil {
		return err
	}
	op.Status = status
	return nil
}

func (r *OperatorRegistry) List() []OperatorProfile {
	out := make([]OperatorProfile, 0, len(r.items))
	for _, item := range r.items {
		cp := *item
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
