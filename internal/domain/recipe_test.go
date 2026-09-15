package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecipeRejectsMutableOrObservedIntent(t *testing.T) {
	base := Recipe{APIVersion: RecipeVersion, Project: "demo", Baseline: "staging", BaselineRevision: "rev1", TTL: "1h", Overrides: map[string]ComponentOverride{"service-b": {BuildID: strings.Repeat("a", 64)}}, Frontends: []RecipeFrontend{}}
	for _, test := range []struct {
		name     string
		override ComponentOverride
		valid    bool
	}{
		{"build", base.Overrides["service-b"], true},
		{"digest", ComponentOverride{Image: "registry.example.com/team/b@sha256:" + strings.Repeat("b", 64)}, true},
		{"tag", ComponentOverride{Image: "registry.example.com/team/b:latest"}, false},
		{"both", ComponentOverride{Image: "registry.example.com/b:v1", BuildID: strings.Repeat("a", 64)}, false},
		{"source", ComponentOverride{BuildID: strings.Repeat("a", 64), Source: &Build{}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := base
			r.Overrides = map[string]ComponentOverride{"service-b": test.override}
			if err := ValidateRecipe(r); (err == nil) != test.valid {
				t.Fatalf("%v", err)
			}
		})
	}

	data, _ := json.Marshal(base)
	for _, raw := range []string{strings.Replace(string(data), "envy/recipe-v1", "envy/recipe-v2", 1), string(data) + " {}", strings.Replace(string(data), `"ttl":"1h"`, `"ttl":"1h","endpoints":{}`, 1)} {
		if _, err := DecodeRecipe([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
