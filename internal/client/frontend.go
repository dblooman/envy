package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func frontendPath(k domain.FrontendKey) (string, error) {
	if err := domain.ValidateFrontendKey(k); err != nil {
		return "", err
	}

	return "/v1/projects/" + k.Project + "/frontend-bindings/" + k.Frontend + "/" + k.Revision, nil
}

func (c *Client) BindFrontend(ctx context.Context, k domain.FrontendKey, req domain.BindFrontendRequest) (domain.FrontendBindingView, error) {
	var out domain.FrontendBindingView
	p, err := frontendPath(k)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPut, p, req, "", &out)
	return out, err
}

func (c *Client) FrontendBinding(ctx context.Context, k domain.FrontendKey) (domain.FrontendBindingView, error) {
	var out domain.FrontendBindingView
	p, err := frontendPath(k)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodGet, p, nil, "", &out)
	return out, err
}

func (c *Client) FrontendBindings(ctx context.Context, id, after string, limit int) (Page[domain.FrontendBindingView], error) {
	p, err := compositionPath(id)
	if err != nil {
		return Page[domain.FrontendBindingView]{}, err
	}

	return catalogList[domain.FrontendBindingView](ctx, c, p+"/frontend-bindings", after, limit)
}

func (c *Client) PublishFrontend(ctx context.Context, k domain.FrontendKey, req domain.PublishFrontendRequest) (domain.FrontendBindingView, error) {
	var out domain.FrontendBindingView
	p, err := frontendPath(k)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPost, p+"/deployment", req, "", &out)
	return out, err
}

func (c *Client) CheckFrontend(ctx context.Context, k domain.FrontendKey, req domain.FrontendCheckRequest) (domain.FrontendBindingView, error) {
	var out domain.FrontendBindingView
	p, err := frontendPath(k)
	if err != nil {
		return out, err
	}

	err = c.request(ctx, http.MethodPost, p+"/check", req, "", &out)
	return out, err
}

// ResolveFrontend never returns a partial URL on timeout or failure. Missing
// associations may arrive asynchronously; gone associations never fall back.
func (c *Client) ResolveFrontend(ctx context.Context, k domain.FrontendKey, timeout time.Duration) (domain.FrontendResolution, error) {
	p, err := frontendPath(k)
	if err != nil {
		return domain.FrontendResolution{}, err
	}

	if timeout < 0 || timeout > 5*time.Minute {
		return domain.FrontendResolution{}, domain.Validation("frontend resolution timeout must be between zero and five minutes")
	}

	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}

	defer cancel()
	for {
		var out domain.FrontendResolution
		err = c.request(waitCtx, http.MethodGet, p+"/resolve", nil, "", &out)
		if err == nil {
			return out, nil
		}

		if ctx.Err() != nil {
			return domain.FrontendResolution{}, ctx.Err()
		}

		if waitCtx.Err() != nil {
			return domain.FrontendResolution{}, &domain.Error{Code: "timeout", Message: "frontend revision did not become resolvable before the deadline", Retryable: true, Project: k.Project}
		}

		var de *domain.Error
		if timeout == 0 || !errors.As(err, &de) || (de.Code != "not_found" && !de.Retryable) {
			return domain.FrontendResolution{}, err
		}

		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
