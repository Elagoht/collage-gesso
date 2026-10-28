package gesso_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestContract runs "collage plugin check -run ." from the collage this module
// requires: the manifest, the constructor plugin add would write, and Name()
// matching collage.json. It builds the CLI, so -short skips it.
func TestContract(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: the contract check builds the collage CLI")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command on PATH")
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*github\.com/Elagoht/collage (v\S+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod requires no github.com/Elagoht/collage")
	}
	cmd := exec.Command(gobin, "run", "github.com/Elagoht/collage/cmd/collage@"+string(m[1]), "plugin", "check", "-run", ".")
	// GOWORK=off: the check runs the released CLI against this module alone, and
	// -mod=mod is refused in workspace mode, which CI runs the plugins in.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("collage plugin check -run .: %v\n%s", err, out)
	}
	for _, want := range []string{"elagoht/gesso: meets the contract", "Name() matches collage.json"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the check did not say %q:\n%s", want, out)
		}
	}
}
