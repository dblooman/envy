package domain

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
)

const RecipeVersion = "envy/recipe-v1"

// Recipe is portable desired intent, not persisted execution state.
type Recipe struct {
	MessageIsolation bool                         `json:"message_isolation,omitempty"`
	APIVersion       string                       `json:"api_version"`
	Project          string                       `json:"project"`
	Baseline         string                       `json:"baseline"`
	BaselineRevision string                       `json:"baseline_revision"`
	Overrides        map[string]ComponentOverride `json:"overrides"`
	TTL              string                       `json:"ttl"`
	Frontends        []RecipeFrontend             `json:"frontends"`
}

type RecipeFrontend struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
}

type RecipeSelection struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

var recipeBuildID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateRecipe(r Recipe) error {
	if r.APIVersion != RecipeVersion || !ValidCatalogID(r.Project) || !ValidCatalogID(r.Baseline) || strings.TrimSpace(r.BaselineRevision) == "" || len(r.BaselineRevision) > 128 {
		return Validation("recipe requires envy/recipe-v1, valid project/baseline and a binding revision")
	}

	if r.Overrides == nil || len(r.Overrides) > MaxOverrides {
		return Validation("recipe requires zero to three overrides")
	}

	for component, o := range r.Overrides {
		if !ValidCatalogID(component) || o.Source != nil {
			return Validation("recipe overrides require valid component IDs and no source observations")
		}

		if o.BuildID != "" {
			if o.Image != "" || !recipeBuildID.MatchString(o.BuildID) {
				return Validation("recipe selects either a build_id or immutable image digest")
			}
		} else {
			if len(o.Image) > 512 {
				return Validation("recipe image reference is too long")
			}

			if _, err := name.NewDigest(o.Image, name.StrictValidation); err != nil {
				return Validation("recipe direct images must use immutable digests; tags cannot be exported")
			}
		}
	}

	if ttl, err := time.ParseDuration(r.TTL); err != nil || ttl <= 0 {
		return Validation("recipe ttl must be a positive Go duration; server limits apply on recreation")
	}

	if len(r.Frontends) > 20 {
		return Validation("select at most twenty recipe frontends")
	}

	seen := map[string]bool{}
	for _, f := range r.Frontends {
		if ValidateFrontendKey(FrontendKey{Project: r.Project, Frontend: f.Name, Revision: f.Revision}) != nil || !PublicFrontendURL(f.Repository, false) {
			return Validation("recipe frontends require valid names, exact commits and HTTPS repository URLs")
		}

		key := f.Name + "/" + f.Revision
		if seen[key] {
			return Validation("recipe has duplicate frontend selections")
		}

		seen[key] = true
	}

	return nil
}

func DecodeRecipe(data []byte) (Recipe, error) {
	var r Recipe
	if len(data) > 64<<10 {
		return r, Validation("recipe must be at most 64 KiB")
	}

	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, Validation("recipe must be JSON with no unknown fields")
	}

	var extra any
	if d.Decode(&extra) != io.EOF {
		return r, Validation("recipe must contain one JSON value")
	}

	return r, ValidateRecipe(r)
}
