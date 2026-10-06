package repository

import (
	"os/exec"
	"testing"
)

func TestPrivateTemplateBudgetPermissionBoundary(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("jq is required to test the private-deployment permission guard")
	}
	cmd := exec.Command("sh", "../../examples/private-deployment/scripts/test-budget-write-plan")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("budget permission tests failed: %v\n%s", err, output)
	}
}
