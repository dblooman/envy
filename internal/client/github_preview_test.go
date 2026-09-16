package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPRPreviewClientPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/github/previews" || r.URL.Query().Get("project") != "demo" || r.URL.Query().Get("after") != "cursor" || r.URL.Query().Get("limit") != "2" {
			t.Errorf("wrong URL %s", r.URL)
		}

		_, _ = w.Write([]byte(`{"items":[],"next_cursor":"next"}`))
	}))
	defer server.Close()
	c, e := New(server.URL, "token", server.Client())
	if e != nil {
		t.Fatal(e)
	}

	out, e := c.PRPreviews(context.Background(), "demo", "cursor", 2)
	if e != nil || out.NextCursor != "next" {
		t.Fatal(out, e)
	}
}
