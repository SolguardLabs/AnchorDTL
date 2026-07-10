package tests

import (
	"testing"

	anchor "github.com/solguardlabs/anchordtl/src"
)

type harness struct {
	engine     *anchor.Engine
	operatorID anchor.OperatorID
	routeID    anchor.RouteID
	guarantee  anchor.GuaranteeID
}

func newHarness(t *testing.T, deposit int64, capacity int64) harness {
	t.Helper()
	engine := anchor.MustNewEngine("aUSDC")
	operatorID, err := engine.RegisterOperator("Atlas Relay", "atlas-controller")
	if err != nil {
		t.Fatal(err)
	}
	spec := anchor.NewRouteSpec(operatorID, "ethereum", "solana", "aUSDC", capacity, 20)
	routeID, err := engine.OpenRoute(spec)
	if err != nil {
		t.Fatal(err)
	}
	guaranteeID, err := engine.DepositGuarantee(operatorID, "primary", deposit)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.BindRouteGuarantee(routeID, guaranteeID); err != nil {
		t.Fatal(err)
	}
	return harness{engine: engine, operatorID: operatorID, routeID: routeID, guarantee: guaranteeID}
}

func TestRouteLifecycleSettlementAndClose(t *testing.T) {
	h := newHarness(t, 1_000_000, 1_000_000)
	obligationID, err := h.engine.OpenObligation(h.routeID, "ticket-001", 400_000, h.engine.Now()+4)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.SettleObligation(obligationID, 400_000); err != nil {
		t.Fatal(err)
	}
	report, err := h.engine.RouteReport(h.routeID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Outstanding.IsZero() {
		t.Fatalf("expected no outstanding amount, got %s", report.Outstanding)
	}
	result, err := h.engine.ReconcileRoute(h.routeID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Closed {
		t.Fatalf("expected route to close")
	}
	route, err := h.engine.Routes.Get(h.routeID)
	if err != nil {
		t.Fatal(err)
	}
	if route.Status != anchor.RouteClosed {
		t.Fatalf("expected closed route, got %s", route.Status)
	}
}

func TestSingleRoutePenaltyAccounting(t *testing.T) {
	h := newHarness(t, 1_000_000, 1_000_000)
	obligationID, err := h.engine.OpenObligation(h.routeID, "ticket-penalty", 600_000, h.engine.Now()+4)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.engine.Slash(anchor.SlashRequest{
		GuaranteeID:   h.guarantee,
		ObligationIDs: []anchor.ObligationID{obligationID},
		Amount:        anchor.NewAmount("aUSDC", 150_000),
		Reason:        anchor.SlashReasonTimeout,
		Memo:          "late finality",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Total.Units != 150_000 {
		t.Fatalf("unexpected slash total %s", receipt.Total)
	}
	guarantee, err := h.engine.Guarantees.Get(h.guarantee)
	if err != nil {
		t.Fatal(err)
	}
	if guarantee.Active.Units != 850_000 {
		t.Fatalf("unexpected active guarantee %s", guarantee.Active)
	}
	view := guarantee.RouteView(h.routeID)
	if view.Penalized.Units != 150_000 {
		t.Fatalf("unexpected route penalty %s", view.Penalized)
	}
	obligation, err := h.engine.Obligations.Get(obligationID)
	if err != nil {
		t.Fatal(err)
	}
	if obligation.Outstanding().Units != 450_000 {
		t.Fatalf("unexpected outstanding amount %s", obligation.Outstanding())
	}
}

func TestMonitorReportsOperationalAlerts(t *testing.T) {
	h := newHarness(t, 500_000, 500_000)
	if _, err := h.engine.OpenObligation(h.routeID, "ticket-monitor", 480_000, h.engine.Now()+1); err != nil {
		t.Fatal(err)
	}
	route, err := h.engine.Routes.Get(h.routeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := route.Pause(); err != nil {
		t.Fatal(err)
	}
	monitor := anchor.NewMonitor(h.engine, anchor.DefaultMonitorRules())
	alerts, err := monitor.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, alert := range alerts {
		codes[alert.Code] = true
	}
	if !codes["route_paused"] {
		t.Fatalf("expected paused route alert, got %#v", alerts)
	}
	if !codes["free_guarantee_low"] {
		t.Fatalf("expected low free guarantee alert, got %#v", alerts)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	h := newHarness(t, 750_000, 750_000)
	if _, err := h.engine.OpenObligation(h.routeID, "ticket-snapshot", 250_000, h.engine.Now()+4); err != nil {
		t.Fatal(err)
	}
	data, err := h.engine.SnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := anchor.DecodeSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Routes) != 1 {
		t.Fatalf("expected one route, got %d", len(snapshot.Routes))
	}
	if len(snapshot.Obligations) != 1 {
		t.Fatalf("expected one obligation, got %d", len(snapshot.Obligations))
	}
}
