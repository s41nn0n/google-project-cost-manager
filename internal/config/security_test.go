package config

import "testing"

func TestLegacyLiveConfigurationIsRejected(t *testing.T) {
	for _, input := range []string{"defaults: {dryRun: false}", "enforcementEnabled: true", "defaults: {threshold: .nan}"} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("accepted unsafe configuration %s", input)
		}
	}
	if _, err := Parse([]byte("defaults: {dryRun: true}")); err != nil {
		t.Fatal(err)
	}
}
