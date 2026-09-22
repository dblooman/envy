package client

import (
	"context"
	"net/http"

	"github.com/dblooman/envy/internal/domain"
)

type SessionResponse struct {
	Principal    domain.Principal `json:"principal"`
	AuthMode     string           `json:"auth_mode"`
	Channel      string           `json:"channel"`
	Capabilities []string         `json:"capabilities"`
}

func (c *Client) Session(ctx context.Context) (SessionResponse, error) {
	var out SessionResponse
	err := c.request(ctx, http.MethodGet, "/v1/session", nil, "", &out)
	return out, err
}
