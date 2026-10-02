package main

import (
	"encoding/json"
	"fmt"
	"github.com/example/google-project-cost-manager/internal/plancheck"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: plancheck PLAN_JSON SCOPE_JSON")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	plan, err := plancheck.Parse(data)
	if err != nil {
		fail(err)
	}
	data, err = os.ReadFile(os.Args[2])
	if err != nil {
		fail(err)
	}
	var scope plancheck.Scope
	if err = json.Unmarshal(data, &scope); err != nil {
		fail(err)
	}
	if err = plancheck.Validate(plan, scope); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
