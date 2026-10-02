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
	path := flag.String("policy", "", "current enforcement policy")
	enable := flag.Bool("enable", false, "enable billing unlink after verified readiness")
	disable := flag.Bool("disable", false, "persist emergency stop independent of CI policy publication")
	confirm := flag.String("confirm", "", "enable requires enable-billing-unlink")
	flag.Parse()
	if *enable == *disable {
		fail(fmt.Errorf("select exactly one of -enable or -disable"))
	}
	cfg, err := config.LoadFile(*path)
	if err != nil {
		fail(err)
	}
	ctx := context.Background()
	if *enable {
		if *confirm != "enable-billing-unlink" || !cfg.EnforcementEnabled || cfg.Defaults.DryRun == nil || *cfg.Defaults.DryRun {
			fail(fmt.Errorf("live reviewed policy and explicit enable-billing-unlink confirmation required"))
		}
		if err = readiness.Verify(ctx, cfg); err != nil {
			fail(err)
		}
	}
	store, err := readiness.New(ctx, cfg)
	if err != nil {
		fail(err)
	}
	defer store.Close()
	if err = store.SetEnabled(ctx, *enable); err != nil {
		fail(err)
	}
	fmt.Printf("Operator enforcement switch enabled=%t\n", *enable)
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
