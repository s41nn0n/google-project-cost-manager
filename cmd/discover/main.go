package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/example/google-project-cost-manager/internal/discovery"
	"gopkg.in/yaml.v3"
)

func main() {
	organization := flag.String("organization", "", "organization resource name (organizations/ID)")
	policyPath := flag.String("policy", "", "reviewed private policy YAML")
	outputDir := flag.String("output", "generated", "output directory")
	flag.Parse()
	if *organization == "" || *policyPath == "" {
		fmt.Fprintln(os.Stderr, "-organization and -policy are required")
		os.Exit(2)
	}
	data, err := os.ReadFile(*policyPath)
	if err != nil {
		fatal(err)
	}
	var policy discovery.ReviewedPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		fatal(err)
	}
	policy.ApplyDefaults()
	if err := policy.Validate(); err != nil {
		fatal(err)
	}
	client, err := discovery.NewGCPClient(context.Background())
	if err != nil {
		fatal(err)
	}
	result, err := discovery.Scan(context.Background(), *organization, policy, client)
	if err != nil {
		fatal(err)
	}
	if err := discovery.WriteOutput(*outputDir, result, policy); err != nil {
		fatal(err)
	}
	if !result.Coverage.Complete {
		fmt.Fprintln(os.Stderr, "discovery completed with blocking diagnostics; see diagnostics.json")
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
