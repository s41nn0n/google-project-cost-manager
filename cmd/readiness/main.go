package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/readiness"
	"os"
)

func main() {
	path := flag.String("policy", "", "generated enforcement policy JSON/YAML")
	flag.Parse()
	cfg, err := config.LoadFile(*path)
	approvedTransition := false
	if err == nil {
		approvedTransition, err = readiness.VerifyWithTransitionStatus(context.Background(), cfg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if approvedTransition {
		fmt.Println("Verified explicit operator transition approval against prior validation and a current reconciliation; no new seven-day streak or billing-toggle test is claimed.")
	} else if cfg.ReadinessMode == config.ReadinessPerProject && cfg.EnforcementEnabled && cfg.Defaults.DryRun != nil && !*cfg.Defaults.DryRun {
		fmt.Println("Verified live-project readiness; dry-run projects remain staged and are not approved for enforcement.")
	} else {
		fmt.Println("Verified seven clean daily reconciliations and disposable-project toggle evidence.")
	}
}
