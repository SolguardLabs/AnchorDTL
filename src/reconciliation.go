package anchordtl

type SolvencyState string

const (
	SolvencyUnknown SolvencyState = "unknown"
	SolvencyHealthy SolvencyState = "healthy"
	SolvencyWatch   SolvencyState = "watch"
	SolvencyDeficit SolvencyState = "deficit"
)

type RouteSolvencyReport struct {
	RouteID             RouteID       `json:"route_id"`
	GuaranteeID         GuaranteeID   `json:"guarantee_id"`
	Epoch               Epoch         `json:"epoch"`
	State               SolvencyState `json:"state"`
	Outstanding         Amount        `json:"outstanding"`
	RouteCoverage       Amount        `json:"route_coverage"`
	FreeGuarantee       Amount        `json:"free_guarantee"`
	DueObligations      int           `json:"due_obligations"`
	OpenObligations     int           `json:"open_obligations"`
	TerminalObligations int           `json:"terminal_obligations"`
	Notes               []string      `json:"notes"`
}

func (r RouteSolvencyReport) Solvent() bool {
	return r.State == SolvencyHealthy || r.State == SolvencyWatch
}

type ReconcileResult struct {
	BatchID       BatchID             `json:"batch_id"`
	RouteReport   RouteSolvencyReport `json:"route_report"`
	Released      Amount              `json:"released"`
	Closed        bool                `json:"closed"`
	ReconciledIDs []ObligationID      `json:"reconciled_ids"`
}

type Reconciler struct {
	Policy      RiskPolicy
	Routes      *RouteBook
	Guarantees  *GuaranteeStore
	Obligations *ObligationBook
	Sink        EventSink
}

func NewReconciler(policy RiskPolicy, routes *RouteBook, guarantees *GuaranteeStore, obligations *ObligationBook, sink EventSink) *Reconciler {
	return &Reconciler{
		Policy:      policy,
		Routes:      routes,
		Guarantees:  guarantees,
		Obligations: obligations,
		Sink:        sink,
	}
}

func (r *Reconciler) BuildRouteReport(routeID RouteID, epoch Epoch) (RouteSolvencyReport, error) {
	route, err := r.Routes.Get(routeID)
	if err != nil {
		return RouteSolvencyReport{}, err
	}
	if route.GuaranteeID == "" {
		return RouteSolvencyReport{}, fail(CodeState, "reconcile.report", "route has no guarantee")
	}
	guarantee, err := r.Guarantees.Get(route.GuaranteeID)
	if err != nil {
		return RouteSolvencyReport{}, err
	}
	outstanding, err := r.Obligations.OutstandingByRoute(routeID, route.Asset())
	if err != nil {
		return RouteSolvencyReport{}, err
	}
	view := guarantee.RouteView(routeID)
	obligations := r.Obligations.ByRoute(routeID)
	report := RouteSolvencyReport{
		RouteID:       routeID,
		GuaranteeID:   route.GuaranteeID,
		Epoch:         epoch,
		State:         SolvencyUnknown,
		Outstanding:   outstanding,
		RouteCoverage: view.Remaining(),
		FreeGuarantee: guarantee.Free(),
		Notes:         make([]string, 0, 4),
	}
	for _, obligation := range obligations {
		if obligation.IsTerminal() {
			report.TerminalObligations++
		} else {
			report.OpenObligations++
		}
		if obligation.Due(epoch) && !obligation.IsTerminal() {
			report.DueObligations++
		}
	}
	switch {
	case report.RouteCoverage.LessThan(report.Outstanding):
		report.State = SolvencyDeficit
		report.Notes = append(report.Notes, "route coverage below outstanding obligations")
	case report.DueObligations > 0:
		report.State = SolvencyWatch
		report.Notes = append(report.Notes, "due obligations require operator action")
	default:
		report.State = SolvencyHealthy
	}
	return report, nil
}

func (r *Reconciler) ReconcileRoute(routeID RouteID, epoch Epoch, closeRoute bool) (ReconcileResult, error) {
	report, err := r.BuildRouteReport(routeID, epoch)
	if err != nil {
		return ReconcileResult{}, err
	}
	if !report.Solvent() {
		return ReconcileResult{}, fail(CodeState, "reconcile.route", "route %s is not solvent", routeID)
	}
	route, err := r.Routes.Get(routeID)
	if err != nil {
		return ReconcileResult{}, err
	}
	guarantee, err := r.Guarantees.Get(report.GuaranteeID)
	if err != nil {
		return ReconcileResult{}, err
	}
	reconciled := make([]ObligationID, 0)
	for _, obligation := range r.Obligations.ByRoute(routeID) {
		if obligation.Outstanding().IsZero() && obligation.State != ObligationClosed {
			item, err := r.Obligations.Get(obligation.ID)
			if err != nil {
				return ReconcileResult{}, err
			}
			item.MarkReconciled(epoch)
			reconciled = append(reconciled, obligation.ID)
		}
	}
	released := ZeroAmount(route.Asset())
	closed := false
	if closeRoute {
		if !guarantee.SolventForRoute(routeID, report.Outstanding) {
			return ReconcileResult{}, fail(CodeState, "reconcile.close", "route %s does not satisfy close coverage", routeID)
		}
		if route.Status == RouteActive || route.Status == RoutePaused {
			if err := route.Drain(); err != nil {
				return ReconcileResult{}, err
			}
		}
		released, err = guarantee.ReleaseRoute(routeID, epoch)
		if err != nil {
			return ReconcileResult{}, err
		}
		if err := route.Close(epoch); err != nil {
			return ReconcileResult{}, err
		}
		closed = true
	}
	result := ReconcileResult{
		BatchID:       NewBatchID(routeID.String(), report.GuaranteeID.String(), "reconcile"),
		RouteReport:   report,
		Released:      released,
		Closed:        closed,
		ReconciledIDs: reconciled,
	}
	if r.Sink != nil {
		event := NewEvent(EventReconciled, epoch, routeID.String()).
			WithRelated(report.GuaranteeID.String()).
			With("state", string(report.State)).
			With("closed", boolString(closed))
		r.Sink.Record(event)
		if closed {
			r.Sink.Record(NewEvent(EventRouteClosed, epoch, routeID.String()).With("released", released.String()))
		}
	}
	return result, nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
