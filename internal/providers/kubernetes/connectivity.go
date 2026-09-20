package kubernetes

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

var shortHost = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

func sensitiveLocation(location string) bool {
	for _, word := range []string{"password", "secret", "token", "credential", "authorization", "api_key", "apikey"} {
		if strings.Contains(strings.ToLower(location), word) {
			return true
		}
	}

	return false
}

func safeQuery(u *url.URL) bool {
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "page", "limit", "offset", "sort":
		default:
			return false
		}
	}

	return true
}

// Parse whole addresses only. Never interpret embedded documents or userinfo.
func connectivityAddress(value string) (string, *url.URL, bool) {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\n\r\t ") {
		return "", nil, false
	}

	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || !safeQuery(u) {
			return "", nil, false
		}

		return u.Hostname(), u, true
	}

	if !strings.Contains(value, ":") {
		return value, nil, true
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", nil, false
	}

	number, err := strconv.Atoi(port)
	return host, nil, err == nil && number > 0 && number <= 65535
}

func plausibleHost(b domain.Baseline, location, host string) bool {
	for _, hint := range []string{"host", "url", "endpoint", "address", "addr", "server", "connection"} {
		if strings.Contains(strings.ToLower(location), hint) {
			return true
		}
	}

	for _, binding := range b.Components {
		service, _, _ := strings.Cut(binding.ServiceHost, ".")
		if strings.EqualFold(service, host) {
			return true
		}
	}

	return false
}

func replaceHost(value, host, replacement string, u *url.URL) string {
	if u == nil {
		return strings.Replace(value, host, replacement, 1)
	}

	copy := *u
	copy.Host = replacement
	if u.Port() != "" {
		copy.Host = net.JoinHostPort(replacement, u.Port())
	}

	return copy.String()
}

func connectivityFindings(b domain.Baseline, location, value string) []domain.ConnectivityFinding {
	if sensitiveLocation(location) {
		return nil
	}

	host, u, ok := connectivityAddress(value)
	if !ok || !shortHost.MatchString(host) || host == "localhost" {
		return nil
	}

	if u == nil && !strings.Contains(value, ":") && !plausibleHost(b, location, host) {
		return nil
	}

	f := domain.ConnectivityFinding{Location: location, Hostname: host, Message: "Short service names resolve in the preview namespace; review this dependency before approval."}
	for _, binding := range b.Components {
		service, _, _ := strings.Cut(binding.ServiceHost, ".")
		if !strings.EqualFold(service, host) || !strings.Contains(binding.ServiceHost, ".") {
			continue
		}

		replacement := replaceHost(value, host, binding.ServiceHost, u)
		if f.Replacement != "" && f.Replacement != replacement {
			f.Replacement = ""
			return []domain.ConnectivityFinding{f}
		}

		f.Replacement = replacement
	}

	return []domain.ConnectivityFinding{f}
}
