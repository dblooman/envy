package api

import (
	"net/http"
	"net/http/httptest"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	envymcp "github.com/dblooman/envy/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool requests enter the same REST validation and activity handlers, in-process.
// The identity is supplied solely by the authenticated outer MCP request.
// No credential or client state is shared between requests or exposed on a port.
type principalTransport struct {
	routes   http.Handler
	identity domain.RequestIdentity
}

func (t principalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(domain.WithRequestIdentity(r.Context(), t.identity))
	r.Header.Del("Authorization")
	w := httptest.NewRecorder()
	t.routes.ServeHTTP(w, r)
	return w.Result(), nil
}

func (h *handler) remoteMCP(routes http.Handler) http.Handler {
	stream := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		identity := domain.RequestIdentityFromContext(r.Context())
		identity.Channel = "mcp"
		c, _ := client.NewWithIdentity("http://envy.internal", "", &http.Client{Transport: principalTransport{routes, identity}}, "mcp", identity.Task)
		return envymcp.NewServer(c)
	}, &sdk.StreamableHTTPOptions{Stateless: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(buildScopeKey{}) != nil {
			writeError(w, &domain.Error{Code: "unauthorized", Message: "build credentials cannot access MCP"})
			return
		}

		if origin := r.Header.Get("Origin"); origin != "" && !csrfAllowed(&http.Request{Method: "POST", Header: r.Header, Host: r.Host, TLS: r.TLS}, h.auth.ExternalOrigin) {
			http.Error(w, "cross-origin MCP request rejected", 403)
			return
		}

		stream.ServeHTTP(w, r)
	})
}
