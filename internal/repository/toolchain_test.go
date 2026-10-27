package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestToolchainPinsMatchModule(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	match := regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)$`).FindStringSubmatch(read("go.mod"))
	if len(match) != 2 {
		t.Fatal("go.mod must specify an exact supported patch version")
	}
	version := match[1]
	if !regexp.MustCompile(`(?m)^FROM golang:` + regexp.QuoteMeta(version) + `@sha256:[0-9a-f]{64} AS build$`).MatchString(read("Dockerfile")) {
		t.Fatal("Docker builder must use the module's exact Go patch version and an immutable digest")
	}
	for _, name := range []string{"ci.yml", "release-artifacts.yml"} {
		matches := regexp.MustCompile(`(?m)go-version:\s*['"]([^'"]+)['"]`).FindAllStringSubmatch(read(filepath.Join(".github", "workflows", name)), -1)
		if len(matches) == 0 {
			t.Fatalf("no pinned Go toolchain found in %s", name)
		}
		for _, pin := range matches {
			if strings.TrimSpace(pin[1]) != version {
				t.Fatalf("%s uses %s instead of module version %s", name, pin[1], version)
			}
		}
	}
}
