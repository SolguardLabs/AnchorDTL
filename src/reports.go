package anchordtl

import (
	"fmt"
	"sort"
	"strings"
)

type OperatorStatement struct {
	Operator        OperatorProfile       `json:"operator"`
	Routes          []Route               `json:"routes"`
	Guarantees      []GuaranteeAccount    `json:"guarantees"`
	OpenExposure    Amount                `json:"open_exposure"`
	TotalSlashed    Amount                `json:"total_slashed"`
	RouteSolvencies []RouteSolvencyReport `json:"route_solvencies"`
}

func (e *Engine) OperatorStatement(operatorID OperatorID) (OperatorStatement, error) {
	operator, err := e.Operators.Get(operatorID)
	if err != nil {
		return OperatorStatement{}, err
	}
	routes := e.Routes.ByOperator(operatorID)
	guarantees := e.Guarantees.ByOperator(operatorID)
	openExposure := ZeroAmount(e.Policy.Asset)
	totalSlashed := ZeroAmount(e.Policy.Asset)
	solvencies := make([]RouteSolvencyReport, 0, len(routes))
	for _, guarantee := range guarantees {
		next, err := totalSlashed.Add(guarantee.Slashed)
		if err != nil {
			return OperatorStatement{}, err
		}
		totalSlashed = next
		for _, exposure := range guarantee.Exposures {
			remaining := exposure.Remaining()
			next, err := openExposure.Add(remaining)
			if err != nil {
				return OperatorStatement{}, err
			}
			openExposure = next
		}
	}
	for _, route := range routes {
		if route.GuaranteeID == "" {
			continue
		}
		report, err := e.RouteReport(route.ID())
		if err != nil {
			return OperatorStatement{}, err
		}
		solvencies = append(solvencies, report)
	}
	sort.Slice(solvencies, func(i, j int) bool { return solvencies[i].RouteID < solvencies[j].RouteID })
	return OperatorStatement{
		Operator:        *operator,
		Routes:          routes,
		Guarantees:      guarantees,
		OpenExposure:    openExposure,
		TotalSlashed:    totalSlashed,
		RouteSolvencies: solvencies,
	}, nil
}

func (s OperatorStatement) Text() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Operator %s (%s)\n", s.Operator.Name, s.Operator.ID))
	b.WriteString(fmt.Sprintf("Status: %s / tier: %s\n", s.Operator.Status, s.Operator.Tier))
	b.WriteString(fmt.Sprintf("Open exposure: %s\n", s.OpenExposure))
	b.WriteString(fmt.Sprintf("Total slashed: %s\n", s.TotalSlashed))
	b.WriteString("Routes:\n")
	for _, route := range s.Routes {
		b.WriteString(fmt.Sprintf("- %s %s -> %s status=%s guarantee=%s\n", route.ID(), route.Spec.Source, route.Spec.Destination, route.Status, route.GuaranteeID))
	}
	b.WriteString("Solvency:\n")
	for _, report := range s.RouteSolvencies {
		b.WriteString(fmt.Sprintf("- %s state=%s outstanding=%s coverage=%s\n", report.RouteID, report.State, report.Outstanding, report.RouteCoverage))
	}
	return b.String()
}

type GuaranteeStatement struct {
	Guarantee   GuaranteeAccount `json:"guarantee"`
	Exposures   []RouteExposure  `json:"exposures"`
	Obligations []Obligation     `json:"obligations"`
}

func (e *Engine) GuaranteeStatement(guaranteeID GuaranteeID) (GuaranteeStatement, error) {
	guarantee, err := e.Guarantees.Get(guaranteeID)
	if err != nil {
		return GuaranteeStatement{}, err
	}
	exposures := make([]RouteExposure, 0, len(guarantee.Exposures))
	for routeID := range guarantee.Exposures {
		exposures = append(exposures, guarantee.RouteView(routeID))
	}
	sort.Slice(exposures, func(i, j int) bool { return exposures[i].RouteID < exposures[j].RouteID })
	return GuaranteeStatement{
		Guarantee:   *guarantee,
		Exposures:   exposures,
		Obligations: e.Obligations.ByGuarantee(guaranteeID),
	}, nil
}

func (s GuaranteeStatement) Text() string {
	var b strings.Builder
	g := s.Guarantee
	b.WriteString(fmt.Sprintf("Guarantee %s operator=%s asset=%s status=%s\n", g.ID, g.OperatorID, g.Asset, g.Status))
	b.WriteString(fmt.Sprintf("Deposited=%s active=%s reserved=%s slashed=%s released=%s\n", g.Deposited, g.Active, g.Reserved, g.Slashed, g.Released))
	for _, exposure := range s.Exposures {
		b.WriteString(fmt.Sprintf("- route=%s reserved=%s remaining=%s settled=%s penalty=%s released=%s util=%s\n",
			exposure.RouteID,
			exposure.Reserved,
			exposure.Remaining(),
			exposure.Settled,
			exposure.Penalized,
			exposure.Released,
			FormatBps(exposure.UtilizationBps()),
		))
	}
	return b.String()
}

type PortfolioHealth struct {
	Epoch           Epoch                 `json:"epoch"`
	Asset           string                `json:"asset"`
	Operators       int                   `json:"operators"`
	Routes          int                   `json:"routes"`
	Guarantees      int                   `json:"guarantees"`
	Obligations     int                   `json:"obligations"`
	OpenExposure    Amount                `json:"open_exposure"`
	ActiveGuarantee Amount                `json:"active_guarantee"`
	Slashed         Amount                `json:"slashed"`
	States          map[SolvencyState]int `json:"states"`
}

func (e *Engine) PortfolioHealth() (PortfolioHealth, error) {
	health := PortfolioHealth{
		Epoch:           e.Now(),
		Asset:           e.Policy.Asset,
		Operators:       len(e.Operators.List()),
		Routes:          len(e.Routes.List()),
		Guarantees:      len(e.Guarantees.List()),
		Obligations:     len(e.Obligations.List()),
		OpenExposure:    ZeroAmount(e.Policy.Asset),
		ActiveGuarantee: ZeroAmount(e.Policy.Asset),
		Slashed:         ZeroAmount(e.Policy.Asset),
		States:          make(map[SolvencyState]int),
	}
	for _, guarantee := range e.Guarantees.List() {
		var err error
		health.ActiveGuarantee, err = health.ActiveGuarantee.Add(guarantee.Active)
		if err != nil {
			return PortfolioHealth{}, err
		}
		health.Slashed, err = health.Slashed.Add(guarantee.Slashed)
		if err != nil {
			return PortfolioHealth{}, err
		}
		for _, exposure := range guarantee.Exposures {
			health.OpenExposure, err = health.OpenExposure.Add(exposure.Remaining())
			if err != nil {
				return PortfolioHealth{}, err
			}
		}
	}
	for _, route := range e.Routes.List() {
		if route.GuaranteeID == "" || route.Status == RouteClosed {
			continue
		}
		report, err := e.RouteReport(route.ID())
		if err != nil {
			return PortfolioHealth{}, err
		}
		health.States[report.State]++
	}
	return health, nil
}
