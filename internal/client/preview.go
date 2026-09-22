package client

import (
	"context"

	"github.com/dblooman/envy/internal/domain"
)

func previewPath(project, baseline, component string) (string, error) {
	for _, id := range []string{project, baseline, component} {
		if !domain.ValidCatalogID(id) {
			return "", domain.Validation("invalid preview profile scope")
		}
	}

	return "/v1/projects/" + project + "/baselines/" + baseline + "/components/" + component + "/preview-profile", nil
}

func (c *Client) DiscoverPreview(ctx context.Context, project, baseline, component string, input domain.PreviewSelection) (domain.PreviewReport, error) {
	var out domain.PreviewReport
	path, err := previewPath(project, baseline, component)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, "POST", path+"/discover", input, "", &out)
	return out, err
}

func (c *Client) ApprovePreview(ctx context.Context, project, baseline, component string, input domain.PreviewApproval) (domain.PreviewProfile, error) {
	var out domain.PreviewProfile
	path, err := previewPath(project, baseline, component)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, "POST", path+"/approve", input, "", &out)
	return out, err
}

func (c *Client) InspectPreview(ctx context.Context, project, baseline, component string) (domain.PreviewProfile, error) {
	var out domain.PreviewProfile
	path, err := previewPath(project, baseline, component)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, "GET", path, nil, "", &out)
	return out, err
}
