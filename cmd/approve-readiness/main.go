// approve-readiness records a deliberate operator transition, never fake evidence.
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
	baselinePath := flag.String("baseline", "", "currently validated live policy")
	targetPath := flag.String("target", "", "reviewed new live policy with one project added")
	project := flag.String("new-project", "", "explicit project addition")
	operator := flag.String("operator", "", "operator responsible for this approval")
	reason := flag.String("reason", "", "reviewed reason for accepting existing validation")
	confirm := flag.String("confirm", "", "must equal accept-existing-validation:PROJECT_ID")
	flag.Parse()
	if *project == "" || *confirm != "accept-existing-validation:"+*project || *operator == "" || *reason == "" {
		fail(fmt.Errorf("explicit project, operator, reason, and accept-existing-validation:PROJECT_ID confirmation required"))
	}
	baseline, err := config.LoadFile(*baselinePath)
	if err != nil {
		fail(err)
	}
	target, err := config.LoadFile(*targetPath)
	if err != nil {
		fail(err)
	}
	if err := readiness.ValidateTransition(baseline, target, *project); err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := readiness.New(ctx, target)
	if err != nil {
		fail(err)
	}
	defer s.Close()
	if err := s.ApproveTransition(ctx, baseline, target, *project, *operator, *reason); err != nil {
		fail(err)
	}
	fmt.Println("Recorded exact-policy operator transition approval; original evidence and test timestamps were not changed.")
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
