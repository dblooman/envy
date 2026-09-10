package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserCORS(t *testing.T) {
	t.Setenv("SHOP_ALLOWED_ORIGIN", "http://localhost:4174")
	h := browserCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, origin := range []string{"http://localhost:4174", "https://hostile.example", ""} {
		r := httptest.NewRequest("GET", "/products", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if (w.Header().Get("Access-Control-Allow-Origin") != "") != (origin == "http://localhost:4174") {
			t.Fatalf("origin %q headers %v", origin, w.Header())
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("unexpected credentials")
		}
	}
}
