package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/dblooman/envy/internal/domain"
	"golang.org/x/oauth2"
)

type googleTransaction struct {
	Nonce    string
	Verifier string
	Browser  string
	Return   string
}

func (s *Server) googleStart(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		http.NotFound(w, r)
		return
	}

	state, nonce, browser := Random(), Random(), Random()
	verifier := oauth2.GenerateVerifier()
	err := transaction(r.Context(), s.pool, func(db *records) error {
		if !s.rate(r.Context(), db, "google-start", 120) {
			return fmt.Errorf("login rate limit")
		}

		return db.put(r.Context(), "google", digest(state), googleTransaction{nonce, verifier, digest(browser), safeReturn(r.URL.Query().Get("return_to"))}, time.Now().Add(10*time.Minute))
	})
	if err != nil {
		http.Error(w, "login unavailable", 503)
		return
	}

	s.cookie(w, "envy_google", browser, 600)
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusSeeOther)
}
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		http.NotFound(w, r)
		return
	}

	var pending googleTransaction
	err := transaction(r.Context(), s.pool, func(db *records) error {
		if err := db.get(r.Context(), "google", digest(r.URL.Query().Get("state")), &pending); err != nil {
			return err
		}

		return db.del(r.Context(), "google", digest(r.URL.Query().Get("state")))
	})
	fail := func() { http.Redirect(w, r, "/login?error=access_denied", http.StatusSeeOther) }
	if err != nil || !equal(pending.Browser, digest(cookieValue(r, s.cookieName("envy_google")))) || r.URL.Query().Get("error") != "" {
		fail()
		return
	}

	s.cookie(w, "envy_google", "", -1)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	token, err := s.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		fail()
		return
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok {
		fail()
		return
	}

	id, err := s.google.Verifier(&oidc.Config{ClientID: s.cfg.GoogleClientID}).Verify(ctx, raw)
	if err != nil || !equal(id.Nonce, pending.Nonce) {
		fail()
		return
	}

	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Domain   string `json:"hd"`
		Name     string `json:"name"`
	}
	if id.Claims(&claims) != nil || !s.admitted(claims.Email, claims.Domain, claims.Verified) {
		fail()
		return
	}

	name := claims.Name
	if name == "" {
		name = claims.Email
	}

	i := Identity{Principal: domain.Principal{Kind: "human", ID: "google:" + digest(id.Issuer+"\x00"+id.Subject), DisplayName: name, Email: claims.Email}, Domain: claims.Domain, Verified: claims.Verified}
	out := newCookieResponse()
	err = transaction(ctx, s.pool, func(db *records) error { return s.issue(ctx, db, out, i) })
	if err != nil {
		http.Error(w, "login unavailable", 503)
		return
	}

	for _, v := range out.Header().Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", v)
	}

	dest := safeReturn(pending.Return)
	if _, err := url.ParseRequestURI(dest); err != nil {
		dest = "/"
	}

	http.Redirect(w, r, dest, http.StatusSeeOther)
}

type cookieResponse struct{ h http.Header }

func newCookieResponse() *cookieResponse              { return &cookieResponse{make(http.Header)} }
func (w *cookieResponse) Header() http.Header         { return w.h }
func (w *cookieResponse) WriteHeader(int)             {}
func (w *cookieResponse) Write(b []byte) (int, error) { return len(b), nil }
