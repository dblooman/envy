package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dblooman/envy/internal/domain"
)

func (c *Client) PlanCreate(ctx context.Context, request domain.CreateRequest) (domain.PreviewPlan, error) {
	var out domain.PreviewPlan
	err := c.request(ctx, http.MethodPost, "/v1/compositions/plan", request, "", &out)
	return out, err
}

func (c *Client) GetOnboardingDraft(ctx context.Context, project string) (domain.OnboardingDraft, error) {
	var out domain.OnboardingDraft
	path, err := catalogPath(project)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodGet, path+"/onboarding-draft", nil, "", &out)
	return out, err
}

func (c *Client) SaveOnboardingDraft(ctx context.Context, draft domain.OnboardingDraft) (domain.OnboardingDraft, error) {
	var out domain.OnboardingDraft
	path, err := catalogPath(draft.Project)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPut, path+"/onboarding-draft", draft, "", &out)
	return out, err
}

func (c *Client) DeleteOnboardingDraft(ctx context.Context, project string, revision int64) error {
	path, err := catalogPath(project)
	if err != nil {
		return err
	}

	if revision < 1 {
		return domain.Validation("draft revision must be positive")
	}

	return c.request(ctx, http.MethodDelete, fmt.Sprintf("%s/onboarding-draft?revision=%d", path, revision), nil, "", nil)
}
