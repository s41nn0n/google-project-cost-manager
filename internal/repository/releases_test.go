package repository

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type releaseStep struct {
	ID   string         `yaml:"id"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

type releaseJob struct {
	Uses        string            `yaml:"uses"`
	If          string            `yaml:"if"`
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Outputs     map[string]string `yaml:"outputs"`
	With        map[string]string `yaml:"with"`
	Steps       []releaseStep     `yaml:"steps"`
}

type releaseWorkflow struct {
	On          map[string]any        `yaml:"on"`
	Permissions map[string]string     `yaml:"permissions"`
	Jobs        map[string]releaseJob `yaml:"jobs"`
}

func readReleaseWorkflow(t *testing.T, name string) releaseWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../.github/workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow releaseWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func TestReleasePublicationContract(t *testing.T) {
	config, err := os.ReadFile("../../release-please-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct{ Draft bool }
	if err := json.Unmarshal(config, &parsed); err != nil || !parsed.Draft {
		t.Fatal("Release Please must create drafts so assets precede publication")
	}
	caller := readReleaseWorkflow(t, "release-please.yml")
	call := caller.Jobs["artifacts"]
	if call.Uses != "./.github/workflows/release-artifacts.yml" || call.Needs != "release-please" || call.If != "needs.release-please.outputs.release_created == 'true'" {
		t.Fatal("artifact publishing must be called directly only for a created draft")
	}
	for input, output := range map[string]string{"tag": "tag_name", "sha": "sha"} {
		if call.With[input] != "${{ needs.release-please.outputs."+output+" }}" || caller.Jobs["release-please"].Outputs[output] != "${{ steps.release.outputs."+output+" }}" {
			t.Fatalf("release %s must come from Release Please's outputs", input)
		}
	}
	if caller.Jobs["release-please"].Outputs["release_created"] != "${{ steps.release.outputs.release_created }}" {
		t.Fatal("artifact invocation must use the created-release output")
	}
	workflow := readReleaseWorkflow(t, "release-artifacts.yml")
	if len(workflow.On) != 1 || workflow.On["workflow_call"] == nil || workflow.Permissions["contents"] != "read" {
		t.Fatal("reusable release workflow must not depend on release events or default write access")
	}
	for _, name := range []string{"scanner", "image"} {
		if workflow.Jobs[name].Needs != "verify" {
			t.Fatalf("%s must wait for source verification", name)
		}
	}
	publish := workflow.Jobs["publish"]
	needs, ok := publish.Needs.([]any)
	if !ok || len(needs) != 2 || needs[0] != "scanner" || needs[1] != "image" || publish.If != "" || len(publish.Permissions) != 1 || publish.Permissions["contents"] != "write" {
		t.Fatal("publication must require both successful artifact jobs and only release write access")
	}
	pinned := regexp.MustCompile(`@[0-9a-f]{40}$`)
	for name, job := range workflow.Jobs {
		sourceChecked := false
		for _, step := range job.Steps {
			if step.Uses != "" && !pinned.MatchString(step.Uses) {
				t.Fatalf("%s contains an unpinned action: %s", name, step.Uses)
			}
			if strings.HasPrefix(step.Uses, "actions/checkout@") && (step.With["ref"] != "${{ inputs.sha }}" || step.With["persist-credentials"] != false) {
				t.Fatalf("%s must check out the exact release SHA without persisted credentials", name)
			}
			if strings.Contains(step.Run, "bash scripts/check-release-source") {
				sourceChecked = true
			}
			if strings.Contains(step.Run, "--draft=false") && (name != "publish" || !strings.HasSuffix(strings.TrimSpace(step.Run), `--draft=false`)) {
				t.Fatal("publishing must be the final operation of the publish job")
			}
		}
		if !sourceChecked {
			t.Fatalf("%s must validate the trusted source", name)
		}
	}
	image := workflow.Jobs["image"]
	scanned := false
	for _, step := range image.Steps {
		if strings.HasPrefix(step.Uses, "docker/build-push-action@") && (step.With["load"] != true || step.With["push"] != false) {
			t.Fatal("build must load locally without pushing before the scan")
		}
		if strings.HasPrefix(step.Uses, "aquasecurity/trivy-action@") {
			scanned = step.With["exit-code"] == "1"
		}
		if strings.Contains(step.Run, "docker push") && !scanned {
			t.Fatal("the scanned image must be pushed only after a passing scan")
		}
	}
	toolsScanned := false
	for _, step := range workflow.Jobs["scanner"].Steps {
		if strings.Contains(step.Run, "go build") && strings.Contains(step.Run, "scripts/check-binary-vulnerabilities") && !strings.Contains(step.Run, "-s -w") {
			toolsScanned = true
		}
		if strings.Contains(step.Run, "scripts/upload-release-assets") && !toolsScanned {
			t.Fatal("operator tools must retain symbols and pass binary scans before upload")
		}
	}
	if !toolsScanned {
		t.Fatal("operator tool build and binary scan are required")
	}
}

func runReleaseScript(t *testing.T, script, dir string, env map[string]string, args ...string) ([]byte, error) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("../../scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd.CombinedOutput()
}

func TestReleaseSourceGuard(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".release-please-manifest.json"), []byte(`{".":"1.0.1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("-c", "init.templateDir=", "init", "--quiet")
	git("add", ".release-please-manifest.json")
	git("-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-c", "user.name=Release Test", "-c", "user.email=release@example.invalid", "commit", "--quiet", "-m", "test release")
	sha := git("rev-parse", "HEAD")
	for _, tc := range []struct {
		name, key, value string
		valid            bool
	}{
		{name: "exact main release", valid: true},
		{name: "wrong version", key: "RELEASE_TAG", value: "v1.0.0"},
		{name: "invalid tag", key: "RELEASE_TAG", value: "v1.0.1;exit 0"},
		{name: "untrusted branch", key: "GITHUB_REF", value: "refs/heads/feature"},
		{name: "tag context", key: "GITHUB_REF", value: "refs/tags/v1.0.1"},
		{name: "wrong workflow SHA", key: "GITHUB_SHA", value: strings.Repeat("a", 40)},
		{name: "wrong checkout", key: "RELEASE_SHA", value: strings.Repeat("b", 40)},
		{name: "wrong checked-out HEAD", key: "HEAD", value: strings.Repeat("b", 40)},
		{name: "invalid SHA", key: "RELEASE_SHA", value: "main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"RELEASE_TAG": "v1.0.1", "RELEASE_SHA": sha, "GITHUB_SHA": sha, "GITHUB_REF": "refs/heads/main"}
			if tc.key == "HEAD" {
				env["RELEASE_SHA"], env["GITHUB_SHA"] = tc.value, tc.value
			} else if tc.key != "" {
				env[tc.key] = tc.value
			}
			out, err := runReleaseScript(t, "check-release-source", dir, env)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t: %v: %s", tc.valid, err, out)
			}
		})
	}
}

func TestReleaseDraftUploadsFailClosedAndRetrySafely(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	guard, err := os.ReadFile("../../scripts/check-release-draft")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts/check-release-draft"), guard, 0700); err != nil {
		t.Fatal(err)
	}
	// Mock gh only; no GitHub token, network, or remote mutations in these tests.
	mock := `#!/usr/bin/env bash
set -eu
case "$2" in
  view)
    if [[ "$*" == *"--json assets"* ]]; then
      [[ "$MOCK_ASSET_QUERY_FAIL" != true ]]
      printf '%s\n' "$MOCK_ASSETS"
    else
      printf '%s\n' "$MOCK_DRAFT"
    fi ;;
  download) printf '%s' "$MOCK_REMOTE_BYTES" ;;
  upload) printf 'UPLOADED\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(mock), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "tool")
	if err := os.WriteFile(file, []byte("expected bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, draft, assets, remote string
		queryFail, valid, upload    bool
	}{
		{name: "new asset", valid: true, upload: true},
		{name: "identical retry", assets: `{"assets":[{"name":"tool"}]}`, remote: "expected bytes", valid: true},
		{name: "differing existing asset", assets: `{"assets":[{"name":"tool"}]}`, remote: "wrong bytes"},
		{name: "published release", draft: `{"isDraft":false,"targetCommitish":"` + sha + `"}`},
		{name: "wrong source draft", draft: `{"isDraft":true,"targetCommitish":"main"}`},
		{name: "asset visibility error", queryFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"PATH":                  dir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"RELEASE_TAG":           "v1.0.1",
				"RELEASE_SHA":           sha,
				"GITHUB_REPOSITORY":     "example/framework",
				"MOCK_DRAFT":            `{"isDraft":true,"targetCommitish":"` + sha + `"}`,
				"MOCK_ASSETS":           `{"assets":[]}`,
				"MOCK_REMOTE_BYTES":     tc.remote,
				"MOCK_ASSET_QUERY_FAIL": "false",
			}
			if tc.draft != "" {
				env["MOCK_DRAFT"] = tc.draft
			}
			if tc.assets != "" {
				env["MOCK_ASSETS"] = tc.assets
			}
			if tc.queryFail {
				env["MOCK_ASSET_QUERY_FAIL"] = "true"
			}
			out, err := runReleaseScript(t, "upload-release-assets", dir, env, file)
			if (err == nil) != tc.valid || strings.Contains(string(out), "UPLOADED") != tc.upload {
				t.Fatalf("valid=%t upload=%t: %v: %s", tc.valid, tc.upload, err, out)
			}
		})
	}
}
