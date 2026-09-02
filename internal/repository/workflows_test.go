package repository

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowYAMLParses(t *testing.T) {
	patterns := []string{"../../.github/workflows/*.yml", "../../examples/private-deployment/.github/workflows/*.yml"}
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil { t.Fatal(err) }
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil { t.Fatal(err) }
			var document yaml.Node
			if err := yaml.Unmarshal(data, &document); err != nil { t.Errorf("%s: %v", file, err) }
		}
	}
}

func TestTerraformProviderLockIsCommittedAtRoot(t *testing.T) {
	if _, err := os.Stat("../../deployments/terraform/.terraform.lock.hcl"); err != nil { t.Fatal(err) }
}
