package config

import "testing"

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte(`budgets: []`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Defaults.Threshold != 0.8 || c.Defaults.DryRun == nil || !*c.Defaults.DryRun || c.UnknownAlertPolicy != "dry_run" {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestExampleConfigParses(t *testing.T) {
	if _, err := LoadFile("../../configs/example.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestParseRejectsBadThreshold(t *testing.T) {
	_, err := Parse([]byte(`defaults:
  threshold: 2
`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseReconcileConfig(t *testing.T) {
	c, err := Parse([]byte(`reconcile:
  enabled: true
  billingAccountName: billingAccounts/123
  requiredPubSubTopic: projects/core/topics/t
selfTest:
  mode: setup_validation
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Reconcile.SourceOfTruth != "gcp_budgets" || c.Reconcile.Mode != "diff_only" {
		t.Fatalf("defaults not applied: %+v", c.Reconcile)
	}
}

func TestParseRejectsBadReconcileConfig(t *testing.T) {
	cases := []string{
		`reconcile: {sourceOfTruth: config}`,
		`reconcile: {mode: mutate}`,
		`reconcile: {enabled: true, requiredPubSubTopic: projects/core/topics/t}`,
		`reconcile: {enabled: true, billingAccountName: billingAccounts/123}`,
	}
	for _, tc := range cases {
		if _, err := Parse([]byte(tc)); err == nil {
			t.Fatalf("expected error for %q", tc)
		}
	}
}

func TestParseRejectsBadActions(t *testing.T) {
	cases := []string{
		`defaults:
  action: email
`,
		`unknownAlertPolicy: email
`,
		`budgets:
- names: [b]
  action: email
`,
		`projects:
  p1:
    action: email
`,
	}
	for _, tc := range cases {
		if _, err := Parse([]byte(tc)); err == nil {
			t.Fatalf("expected error for %q", tc)
		}
	}
}
