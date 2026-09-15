package authn

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ory/fosite"
)

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	base := s.cfg.Origin
	if strings.Contains(r.URL.Path, "oauth-protected-resource") {
		resource := "/mcp"
		if strings.HasSuffix(r.URL.Path, "/v1") {
			resource = "/v1"
		}

		jsonResponse(w, map[string]any{"resource": base + resource, "authorization_servers": []string{base}, "scopes_supported": []string{"envy", "offline_access"}, "bearer_methods_supported": []string{"header"}})
		return
	}

	jsonResponse(w, map[string]any{"issuer": base, "authorization_endpoint": base + "/oauth/authorize", "token_endpoint": base + "/oauth/token", "registration_endpoint": base + "/oauth/register", "revocation_endpoint": base + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"envy", "offline_access"}})
}
func validRedirect(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || (u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback()))
}
func (s *Server) register(w http.ResponseWriter, r *http.Request, db *records) error {
	if !s.rate(r.Context(), db, "register", 30) {
		http.Error(w, "try again later", 429)
		return nil
	}

	// Expiry bounds anonymous registration storage. Active client grants are shorter.
	if _, err := db.tx.Exec(r.Context(), "DELETE FROM envy_auth_records WHERE expires_at<now()"); err != nil {
		return err
	}

	var count int
	if err := db.tx.QueryRow(r.Context(), "SELECT count(*) FROM envy_auth_records WHERE kind='client'").Scan(&count); err != nil {
		return err
	}

	if count >= 1000 {
		http.Error(w, "registration capacity reached", 429)
		return nil
	}

	var in struct {
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
		Auth      string   `json:"token_endpoint_auth_method"`
		Grants    []string `json:"grant_types"`
		Responses []string `json:"response_types"`
	}
	fail := func() { w.WriteHeader(400); jsonResponse(w, map[string]string{"error": "invalid_client_metadata"}) }
	if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Name) > 200 || len(in.Redirects) == 0 || len(in.Redirects) > 10 || (in.Auth != "" && in.Auth != "none") {
		fail()
		return nil
	}

	for _, g := range in.Grants {
		if g != "authorization_code" && g != "refresh_token" {
			fail()
			return nil
		}
	}

	for _, v := range in.Responses {
		if v != "code" {
			fail()
			return nil
		}
	}

	for _, v := range in.Redirects {
		if !validRedirect(v) {
			fail()
			return nil
		}
	}

	id := Random()
	c := fosite.DefaultClient{ID: id, Public: true, RedirectURIs: in.Redirects, GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, Scopes: []string{"envy", "offline_access"}, Audience: []string{s.cfg.Origin + "/v1", s.cfg.Origin + "/mcp"}}
	if err := db.put(r.Context(), "client", id, c, time.Now().Add(90*24*time.Hour)); err != nil {
		return err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "MCP application"
	}

	if err := db.put(r.Context(), "client-name", id, name, time.Now().Add(90*24*time.Hour)); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	jsonResponse(w, map[string]any{"client_id": id, "client_name": name, "redirect_uris": in.Redirects, "token_endpoint_auth_method": "none", "grant_types": c.GrantTypes, "response_types": c.ResponseTypes})
	return nil
}

type pendingConsent struct {
	Query   string
	Browser string
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, db *records) error {
	ctx := r.Context()
	i, err := s.browser(ctx, db, r)
	if err != nil {
		http.Redirect(w, r, "/login?return_to="+url.QueryEscape(safeReturn(r.URL.RequestURI())), http.StatusSeeOther)
		return nil
	}

	var pending pendingConsent
	decision := ""
	if r.Method == "POST" {
		if !s.csrf(w, r) {
			return nil
		}

		decision = r.FormValue("decision")
		key := r.FormValue("pending")
		if db.get(ctx, "consent", digest(key), &pending) != nil || !equal(pending.Browser, digest(cookieValue(r, s.cookieName(browserCookie)))) {
			http.Error(w, "authorization expired", 400)
			return nil
		}

		if err := db.del(ctx, "consent", digest(key)); err != nil {
			return err
		}

		r = r.Clone(ctx)
		r.Method = "GET"
		r.URL = &url.URL{Path: "/oauth/authorize", RawQuery: pending.Query}
		r.Form = nil
		r.PostForm = nil
		r.Body = http.NoBody
	}

	provider := s.provider(db)
	req, err := provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		provider.WriteAuthorizeError(ctx, w, req, err)
		return nil
	}

	values := r.URL.Query()
	resource := values.Get("resource")
	if len(values["resource"]) != 1 || (resource != s.cfg.Origin+"/v1" && resource != s.cfg.Origin+"/mcp") || values.Get("code_challenge_method") != "S256" || len(values.Get("code_challenge")) != 43 || values.Get("response_type") != "code" {
		provider.WriteAuthorizeError(ctx, w, req, fosite.ErrInvalidRequest)
		return nil
	}

	if !req.GetRequestedScopes().Has("envy") {
		provider.WriteAuthorizeError(ctx, w, req, fosite.ErrInvalidScope)
		return nil
	}

	if decision == "" {
		id := Random()
		if err := db.put(ctx, "consent", digest(id), pendingConsent{r.URL.RawQuery, digest(cookieValue(r, s.cookieName(browserCookie)))}, time.Now().Add(10*time.Minute)); err != nil {
			return err
		}

		var name string
		_ = db.get(ctx, "client-name", req.GetClient().GetID(), &name)
		if name == "" {
			name = req.GetClient().GetID()
		}

		csrf := cookieValue(r, s.cookieName(csrfCookie))
		if csrf == "" {
			csrf = Random()
			s.cookie(w, csrfCookie, csrf, 7*24*3600)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		return consentPage.Execute(w, map[string]string{"Name": name, "User": i.Principal.DisplayName, "Pending": id, "CSRF": csrf})
	}

	if decision != "allow" {
		provider.WriteAuthorizeError(ctx, w, req, fosite.ErrAccessDenied)
		return nil
	}

	for _, v := range req.GetRequestedScopes() {
		req.GrantScope(v)
	}

	sess := &oauthSession{Identity: i, Resource: resource, Until: time.Now().Add(30 * 24 * time.Hour)}
	sess.Subject = i.Principal.ID
	sess.Username = i.Principal.DisplayName
	response, err := provider.NewAuthorizeResponse(ctx, req, sess)
	if err != nil {
		provider.WriteAuthorizeError(ctx, w, req, err)
		return nil
	}

	provider.WriteAuthorizeResponse(ctx, w, req, response)
	return nil
}
func (s *Server) token(w http.ResponseWriter, r *http.Request, db *records) error {
	p := s.provider(db)
	req, err := p.NewAccessRequest(r.Context(), r, &oauthSession{})
	if err != nil {
		p.WriteAccessError(r.Context(), w, req, err)
		return nil
	}

	sess, ok := req.GetSession().(*oauthSession)
	resource := r.PostForm["resource"]
	if !ok || len(resource) != 1 || resource[0] != sess.Resource {
		p.WriteAccessError(r.Context(), w, req, fosite.ErrInvalidRequest)
		return nil
	}

	response, err := p.NewAccessResponse(r.Context(), req)
	if err != nil {
		p.WriteAccessError(r.Context(), w, req, err)
		return nil
	}

	p.WriteAccessResponse(r.Context(), w, req, response)
	return nil
}
func (s *Server) revoke(w http.ResponseWriter, r *http.Request, db *records) error {
	p := s.provider(db)
	err := p.NewRevocationRequest(r.Context(), r)
	p.WriteRevocationResponse(r.Context(), w, err)
	return nil
}
