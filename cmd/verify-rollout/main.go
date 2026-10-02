// verify-rollout runs an explicitly authorized destructive test, never a scheduled runtime action.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/readiness"
	"github.com/example/google-project-cost-manager/internal/selftest"
	"os"
	"regexp"
	"time"
)

func main() {
	path := flag.String("policy", "", "generated enforcement policy JSON/YAML")
	project := flag.String("disposable-project", "", "explicit disposable project ID")
	account := flag.String("billing-account", "", "expected billingAccounts/ID")
	confirmation := flag.String("confirm", "", "must equal billing-toggle:PROJECT_ID; billing loss can destroy resources")
	flag.Parse()
	if *project == "" || *confirmation != "billing-toggle:"+*project || *account == "" {
		fail(fmt.Errorf("explicit disposable-project, account and destructive confirmation required"))
	}
	cfg, err := config.LoadFile(*path)
	if err != nil {
		fail(err)
	}
	if *project == cfg.ControlProjectID {
		fail(fmt.Errorf("control project is protected"))
	}
	if err = readiness.CheckFresh(cfg, time.Now().UTC()); err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := billing.NewCloudClient(ctx)
	if err != nil {
		fail(err)
	}
	cfg.SelfTest = config.SelfTestConfig{Enabled: true, TestProjectID: *project, BillingAccountName: *account, AllowedProjectIDPattern: "^" + regexp.QuoteMeta(*project) + "$"}
	result := selftest.Run(ctx, "billing_toggle", cfg, client, &selftest.MemoryStore{})
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if result.FinalState != "billing_enabled" {
		fail(fmt.Errorf("toggle failed; inspect disposable project's billing and restore explicitly"))
	}
	for _, step := range result.Steps {
		if !step.OK {
			fail(fmt.Errorf("test step %s failed", step.Name))
		}
	}
	// Restore the reviewed configuration before hashing its evidence identity.
	cfg, err = config.LoadFile(*path)
	if err != nil {
		fail(err)
	}
	store, err := readiness.New(ctx, cfg)
	if err != nil {
		fail(err)
	}
	defer store.Close()
	if err = store.RecordToggle(ctx, cfg, *project, time.Now().UTC()); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
