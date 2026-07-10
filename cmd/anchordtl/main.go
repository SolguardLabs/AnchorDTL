package main

import (
	"fmt"
	"os"

	anchor "github.com/solguardlabs/anchordtl/src"
)

func main() {
	command := "demo"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "demo":
		if err := runDemo(false); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "snapshot":
		if err := runDemo(true); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: anchordtl [demo|snapshot]")
		os.Exit(2)
	}
}

func runDemo(snapshot bool) error {
	engine := anchor.MustNewEngine("aUSDC")
	operatorID, err := engine.RegisterOperator("North Relay", "north-controller")
	if err != nil {
		return err
	}
	spec := anchor.NewRouteSpec(operatorID, "ethereum", "solana", "aUSDC", 1_000_000, 25)
	routeID, err := engine.OpenRoute(spec)
	if err != nil {
		return err
	}
	guaranteeID, err := engine.DepositGuarantee(operatorID, "main", 1_500_000)
	if err != nil {
		return err
	}
	if err := engine.BindRouteGuarantee(routeID, guaranteeID); err != nil {
		return err
	}
	obligationID, err := engine.OpenObligation(routeID, "demo-ticket-001", 400_000, engine.Now()+5)
	if err != nil {
		return err
	}
	if err := engine.SettleObligation(obligationID, 400_000); err != nil {
		return err
	}
	if _, err := engine.ReconcileRoute(routeID, true); err != nil {
		return err
	}
	if snapshot {
		data, err := engine.SnapshotJSON()
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	statement, err := engine.OperatorStatement(operatorID)
	if err != nil {
		return err
	}
	fmt.Print(statement.Text())
	return nil
}
