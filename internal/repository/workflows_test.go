package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowYAMLParses(t *testing.T) {
	patterns := []string{"../../.github/workflows/*.yml", "../../examples/private-deployment/.github/workflows/*.yml"}
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var document yaml.Node
			if err := yaml.Unmarshal(data, &document); err != nil {
				t.Errorf("%s: %v", file, err)
			}
		}
	}
}

func TestTerraformProviderLockIsCommittedAtRoot(t *testing.T) {
	if _, err := os.Stat("../../deployments/terraform/.terraform.lock.hcl"); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleProviderVersionContract(t *testing.T) {
	// Keep standalone modules and the root aligned with the tested major-8 release.
	constraint := `">= 8.5.0, < 9.0.0"`
	lockedVersion := regexp.MustCompile(`version\s*=\s*"8\.5\.0"`)
	for _, root := range []string{
		"../../deployments/terraform",
		"../../deployments/terraform/modules/bootstrap",
		"../../deployments/terraform/modules/control-plane",
		"../../deployments/terraform/modules/billing-account",
		"../../deployments/terraform/modules/iam-onboarding",
	} {
		t.Run(filepath.Base(root), func(t *testing.T) {
			versions, err := os.ReadFile(filepath.Join(root, "versions.tf"))
			if err != nil {
				t.Fatal(err)
			}
			lock, err := os.ReadFile(filepath.Join(root, ".terraform.lock.hcl"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(versions), constraint) || !strings.Contains(string(lock), constraint) || !lockedVersion.Match(lock) {
				t.Fatal("Google provider constraints and locks must use the tested 8.5.0 contract")
			}
		})
	}
}
