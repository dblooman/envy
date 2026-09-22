package domain

import "testing"

func TestPreviewProvenanceIncludesOnlyDeclaredSharedDependencies(t *testing.T) {
	c := Composition{Overrides: map[string]ComponentOverride{"api": {Image: "v2"}}, Runtime: RuntimeState{Plan: &ResolvedPlan{Previews: map[string]PreviewSnapshot{"api": {Revision: 2, CompositePolicy: &CompositePreviewPolicy{SharedDependencies: []string{"shared database"}}}}}}}
	c.RefreshPreviewProvenance()
	if len(c.PreviewProfiles["api"].SharedDependencies) != 1 {
		t.Fatal("declared dependency missing")
	}

	c.Runtime.Plan.Previews["api"].CompositePolicy.SharedDependencies[0] = "changed"
	if c.PreviewProfiles["api"].SharedDependencies[0] != "shared database" {
		t.Fatal("provenance shares mutable policy slice")
	}
}
