package buildinfo

import "testing"

func TestSourceBuildDefaults(t *testing.T) {
	if Version != "dev" || Commit != "unknown" {
		t.Fatalf("source defaults = version %q commit %q", Version, Commit)
	}
}
