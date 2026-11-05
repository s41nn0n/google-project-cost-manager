// qualify-system explicitly accepts completed validation for a reviewed release.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/readiness"
)

func main() {
	baselinePath := flag.String("baseline", "", "original validated policy, unchanged")
	targetPath := flag.String("target", "", "reviewed release's complete per-project policy")
	approval := flag.String("validation-approval", "", "historical transition approval fingerprint")
	operator := flag.String("operator", "", "responsible operator")
	reason := flag.String("reason", "", "reason for accepting initial system validation")
	confirm := flag.String("confirm", "", "must equal qualify-reviewed-system")
	flag.Parse()
	if *confirm != "qualify-reviewed-system" {
		fail(fmt.Errorf("explicit qualify-reviewed-system confirmation is required"))
	}
	baseline, err := config.LoadFile(*baselinePath)
	if err != nil {
		fail(err)
	}
	target, err := config.LoadFile(*targetPath)
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := readiness.New(ctx, target)
	if err != nil {
		fail(err)
	}
	defer s.Close()
	if err := s.QualifySystem(ctx, baseline, target, *approval, *operator, *reason); err != nil {
		fail(err)
	}
	fmt.Printf("Recorded %s. Initial validation is referenced; live enforcement still requires fresh inventory, current project checks and the operator switch.\n", readiness.SystemFingerprint(target))
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
