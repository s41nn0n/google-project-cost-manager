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
	if err == nil {
		err = readiness.Verify(context.Background(), cfg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Verified seven clean daily reconciliations and disposable-project toggle evidence.")
}
