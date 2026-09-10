// Package github resolves source revisions using repository-scoped GitHub App tokens.
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type Provider struct {
	appID   string
	key     *rsa.PrivateKey
	client  *http.Client
	baseURL string
}

func New(appID string, keyPEM []byte) (*Provider, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil || appID == "" {
		return nil, fmt.Errorf("GitHub App ID and RSA private key are required")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, fmt.Errorf("invalid GitHub App private key")
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("GitHub App key must be RSA")
		}
	}
	return &Provider{appID: appID, key: key, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, baseURL: "https://api.github.com"}, nil
}
func (p *Provider) jwt() (string, error) {
	now := time.Now()
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(8 * time.Minute).Unix(), "iss": p.appID})
	data := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(data))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return data + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
func (p *Provider) request(ctx context.Context, method, path, token string, input, output any) error {
	var data []byte
	if input != nil {
		data, _ = json.Marshal(input)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	res, err := p.client.Do(req)
	if err != nil {
		return &domain.Error{Code: "unavailable", Message: "GitHub request failed", Retryable: true}
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return domain.NotFound("repository or revision is not accessible through the GitHub App")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &domain.Error{Code: "unavailable", Message: "GitHub could not authorize or complete the lookup; check installation access and rate limits", Retryable: res.StatusCode == 429 || res.StatusCode >= 500}
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(output); err != nil {
		return &domain.Error{Code: "unavailable", Message: "invalid GitHub response"}
	}
	return nil
}
func (p *Provider) token(ctx context.Context, r domain.SourceRepository) (string, error) {
	jwt, err := p.jwt()
	if err != nil {
		return "", err
	}
	var out struct {
		Token string `json:"token"`
	}
	parts := strings.Split(r.GitHubRepository, "/")
	if len(parts) != 2 {
		return "", domain.Validation("invalid GitHub repository")
	}
	err = p.request(ctx, "POST", "/app/installations/"+strconv.FormatInt(r.InstallationID, 10)+"/access_tokens", jwt, map[string]any{"repositories": []string{parts[1]}, "permissions": map[string]string{"contents": "read"}}, &out)
	if err == nil && out.Token == "" {
		err = &domain.Error{Code: "unavailable", Message: "GitHub returned no installation token"}
	}
	return out.Token, err
}
func repoPath(r domain.SourceRepository) string {
	parts := strings.SplitN(r.GitHubRepository, "/", 2)
	return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
}
func (p *Provider) Check(ctx context.Context, r domain.SourceRepository) error {
	token, err := p.token(ctx, r)
	if err != nil {
		return err
	}
	var out struct {
		FullName string `json:"full_name"`
	}
	if err = p.request(ctx, "GET", repoPath(r), token, nil, &out); err != nil {
		return err
	}
	if !strings.EqualFold(out.FullName, r.GitHubRepository) {
		return domain.Validation("GitHub repository identity has changed; update onboarding")
	}
	return nil
}

type commitResponse struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

func (p *Provider) Resolve(ctx context.Context, r domain.SourceRepository, ref string) (domain.GitCommit, error) {
	token, err := p.token(ctx, r)
	if err != nil {
		return domain.GitCommit{}, err
	}
	// Full SHAs resolve directly. Every other input is explicitly a branch, never a short SHA or tag.
	path := repoPath(r) + "/commits/" + url.PathEscape(ref)
	if len(ref) != 40 || strings.Trim(ref, "0123456789abcdef") != "" {
		path = repoPath(r) + "/commits/" + url.PathEscape("refs/heads/"+ref)
	}
	var out commitResponse
	err = p.request(ctx, "GET", path, token, nil, &out)
	if err == nil && (len(out.SHA) != 40 || strings.Trim(out.SHA, "0123456789abcdef") != "") {
		err = &domain.Error{Code: "unavailable", Message: "GitHub returned an invalid commit SHA"}
	}
	return domain.GitCommit{SHA: out.SHA, Message: out.Commit.Message}, err
}
func (p *Provider) Branches(ctx context.Context, r domain.SourceRepository, page int) ([]domain.GitBranch, error) {
	token, err := p.token(ctx, r)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	err = p.request(ctx, "GET", repoPath(r)+"/branches?per_page=30&page="+strconv.Itoa(page), token, nil, &rows)
	out := []domain.GitBranch{}
	for _, row := range rows {
		out = append(out, domain.GitBranch{Name: row.Name, SHA: row.Commit.SHA})
	}
	return out, err
}
func (p *Provider) Commits(ctx context.Context, r domain.SourceRepository, branch string, page int) ([]domain.GitCommit, error) {
	token, err := p.token(ctx, r)
	if err != nil {
		return nil, err
	}
	var rows []commitResponse
	q := url.Values{"sha": {"refs/heads/" + branch}, "per_page": {"30"}, "page": {strconv.Itoa(page)}}
	err = p.request(ctx, "GET", repoPath(r)+"/commits?"+q.Encode(), token, nil, &rows)
	out := []domain.GitCommit{}
	for _, row := range rows {
		out = append(out, domain.GitCommit{SHA: row.SHA, Message: row.Commit.Message})
	}
	return out, err
}
