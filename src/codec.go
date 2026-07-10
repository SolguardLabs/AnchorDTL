package anchordtl

import "encoding/json"

type EngineSnapshot struct {
	Policy      RiskPolicy         `json:"policy"`
	Epoch       Epoch              `json:"epoch"`
	Operators   []OperatorProfile  `json:"operators"`
	Routes      []Route            `json:"routes"`
	Guarantees  []GuaranteeAccount `json:"guarantees"`
	Obligations []Obligation       `json:"obligations"`
	Ledger      []LedgerEntry      `json:"ledger"`
	Events      []Event            `json:"events"`
}

func (e *Engine) Snapshot() EngineSnapshot {
	return EngineSnapshot{
		Policy:      e.Policy,
		Epoch:       e.Now(),
		Operators:   e.Operators.List(),
		Routes:      e.Routes.List(),
		Guarantees:  e.Guarantees.List(),
		Obligations: e.Obligations.List(),
		Ledger:      e.Ledger.Entries(),
		Events:      e.Events.All(),
	}
}

func (s EngineSnapshot) JSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

func (e *Engine) SnapshotJSON() ([]byte, error) {
	return e.Snapshot().JSON()
}

func DecodeSnapshot(data []byte) (EngineSnapshot, error) {
	var snapshot EngineSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return EngineSnapshot{}, wrap(CodeInvalid, "codec.decode", err, "invalid snapshot json")
	}
	return snapshot, nil
}

type PublicRouteView struct {
	Route       Route               `json:"route"`
	Report      RouteSolvencyReport `json:"report"`
	Exposure    RouteExposure       `json:"exposure"`
	Obligations []Obligation        `json:"obligations"`
}

func (e *Engine) RouteView(routeID RouteID) (PublicRouteView, error) {
	route, err := e.Routes.Get(routeID)
	if err != nil {
		return PublicRouteView{}, err
	}
	report, err := e.RouteReport(routeID)
	if err != nil {
		return PublicRouteView{}, err
	}
	guarantee, err := e.Guarantees.Get(route.GuaranteeID)
	if err != nil {
		return PublicRouteView{}, err
	}
	return PublicRouteView{
		Route:       *route,
		Report:      report,
		Exposure:    guarantee.RouteView(routeID),
		Obligations: e.Obligations.ByRoute(routeID),
	}, nil
}

func (v PublicRouteView) JSON() ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
