package anchordtl

import "sort"

type AlertLevel string

const (
	AlertInfo     AlertLevel = "info"
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

type Alert struct {
	Level   AlertLevel `json:"level"`
	Subject string     `json:"subject"`
	Code    string     `json:"code"`
	Message string     `json:"message"`
	Epoch   Epoch      `json:"epoch"`
}

type MonitorRules struct {
	MaxRouteUtilizationBps int64 `json:"max_route_utilization_bps"`
	MinFreeGuaranteeBps    int64 `json:"min_free_guarantee_bps"`
	MaxDueObligations      int   `json:"max_due_obligations"`
	RequireActiveRoutes    bool  `json:"require_active_routes"`
}

func DefaultMonitorRules() MonitorRules {
	return MonitorRules{
		MaxRouteUtilizationBps: 8_500,
		MinFreeGuaranteeBps:    1_000,
		MaxDueObligations:      0,
		RequireActiveRoutes:    true,
	}
}

type Monitor struct {
	Engine *Engine
	Rules  MonitorRules
}

func NewMonitor(engine *Engine, rules MonitorRules) Monitor {
	return Monitor{Engine: engine, Rules: rules}
}

func (m Monitor) Evaluate() ([]Alert, error) {
	alerts := make([]Alert, 0)
	for _, route := range m.Engine.Routes.List() {
		items, err := m.EvaluateRoute(route.ID())
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, items...)
	}
	for _, guarantee := range m.Engine.Guarantees.List() {
		items, err := m.EvaluateGuarantee(guarantee.ID)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, items...)
	}
	for _, operator := range m.Engine.Operators.List() {
		items, err := m.EvaluateOperator(operator.ID)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, items...)
	}
	sortAlerts(alerts)
	return alerts, nil
}

func (m Monitor) EvaluateRoute(routeID RouteID) ([]Alert, error) {
	route, err := m.Engine.Routes.Get(routeID)
	if err != nil {
		return nil, err
	}
	alerts := make([]Alert, 0, 3)
	if m.Rules.RequireActiveRoutes && route.Status == RoutePaused {
		alerts = append(alerts, m.alert(AlertWarning, routeID.String(), "route_paused", "route is paused"))
	}
	if route.Status == RouteClosed {
		return alerts, nil
	}
	if route.GuaranteeID == "" {
		alerts = append(alerts, m.alert(AlertCritical, routeID.String(), "route_unbacked", "route has no bound guarantee"))
		return alerts, nil
	}
	report, err := m.Engine.RouteReport(routeID)
	if err != nil {
		return nil, err
	}
	switch report.State {
	case SolvencyDeficit:
		alerts = append(alerts, m.alert(AlertCritical, routeID.String(), "route_deficit", "route coverage below outstanding amount"))
	case SolvencyWatch:
		alerts = append(alerts, m.alert(AlertWarning, routeID.String(), "route_watch", "route has due obligations"))
	}
	if report.DueObligations > m.Rules.MaxDueObligations {
		alerts = append(alerts, m.alert(AlertWarning, routeID.String(), "due_obligations", "route has due obligations above rule threshold"))
	}
	guarantee, err := m.Engine.Guarantees.Get(route.GuaranteeID)
	if err != nil {
		return nil, err
	}
	exposure := guarantee.RouteView(routeID)
	if exposure.UtilizationBps() > m.Rules.MaxRouteUtilizationBps {
		alerts = append(alerts, m.alert(AlertWarning, routeID.String(), "route_utilization", "route guarantee utilization above rule threshold"))
	}
	return alerts, nil
}

func (m Monitor) EvaluateGuarantee(guaranteeID GuaranteeID) ([]Alert, error) {
	guarantee, err := m.Engine.Guarantees.Get(guaranteeID)
	if err != nil {
		return nil, err
	}
	alerts := make([]Alert, 0, 4)
	if guarantee.Status == GuaranteeFrozen {
		alerts = append(alerts, m.alert(AlertWarning, guaranteeID.String(), "guarantee_frozen", "guarantee account is frozen"))
	}
	if guarantee.Status == GuaranteeExhausted {
		alerts = append(alerts, m.alert(AlertCritical, guaranteeID.String(), "guarantee_exhausted", "guarantee account is exhausted"))
	}
	if guarantee.Deposited.Units > 0 {
		freeBps := (guarantee.Free().Units * 10_000) / guarantee.Deposited.Units
		if freeBps < m.Rules.MinFreeGuaranteeBps && guarantee.Status == GuaranteeOpen {
			alerts = append(alerts, m.alert(AlertWarning, guaranteeID.String(), "free_guarantee_low", "free guarantee below rule threshold"))
		}
	}
	if guarantee.Active.LessThan(guarantee.Reserved) {
		alerts = append(alerts, m.alert(AlertCritical, guaranteeID.String(), "reserved_above_active", "reserved guarantee exceeds active balance"))
	}
	return alerts, nil
}

func (m Monitor) EvaluateOperator(operatorID OperatorID) ([]Alert, error) {
	operator, err := m.Engine.Operators.Get(operatorID)
	if err != nil {
		return nil, err
	}
	alerts := make([]Alert, 0, 4)
	switch operator.Status {
	case OperatorSuspended:
		alerts = append(alerts, m.alert(AlertCritical, operatorID.String(), "operator_suspended", "operator is suspended"))
	case OperatorRetired:
		alerts = append(alerts, m.alert(AlertInfo, operatorID.String(), "operator_retired", "operator is retired"))
	}
	statement, err := m.Engine.OperatorStatement(operatorID)
	if err != nil {
		return nil, err
	}
	if len(statement.Routes) == 0 {
		alerts = append(alerts, m.alert(AlertInfo, operatorID.String(), "operator_no_routes", "operator has no routes"))
	}
	openRoutes := 0
	for _, route := range statement.Routes {
		if route.IsOpen() {
			openRoutes++
		}
	}
	if openRoutes == 0 && operator.Status == OperatorActive {
		alerts = append(alerts, m.alert(AlertWarning, operatorID.String(), "operator_no_open_routes", "active operator has no open routes"))
	}
	return alerts, nil
}

func (m Monitor) alert(level AlertLevel, subject string, code string, message string) Alert {
	return Alert{
		Level:   level,
		Subject: subject,
		Code:    code,
		Message: message,
		Epoch:   m.Engine.Now(),
	}
}

func sortAlerts(alerts []Alert) {
	priority := map[AlertLevel]int{
		AlertCritical: 0,
		AlertWarning:  1,
		AlertInfo:     2,
	}
	sort.Slice(alerts, func(i, j int) bool {
		pi := priority[alerts[i].Level]
		pj := priority[alerts[j].Level]
		if pi != pj {
			return pi < pj
		}
		if alerts[i].Subject != alerts[j].Subject {
			return alerts[i].Subject < alerts[j].Subject
		}
		return alerts[i].Code < alerts[j].Code
	})
}
