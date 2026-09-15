// Package authn owns browser sessions and Envy-issued OAuth credentials.
package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/fosite"
)

type records struct{ tx pgx.Tx }

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (s *records) get(ctx context.Context, kind, key string, out any) error {
	var b []byte
	err := s.tx.QueryRow(ctx, "SELECT body FROM envy_auth_records WHERE kind=$1 AND key=$2 AND expires_at>now()", kind, key).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return fosite.ErrNotFound
	}

	if err != nil {
		return err
	}

	return json.Unmarshal(b, out)
}
func (s *records) put(ctx context.Context, kind, key string, v any, until time.Time) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	_, err = s.tx.Exec(ctx, "INSERT INTO envy_auth_records(kind,key,body,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(kind,key) DO UPDATE SET body=excluded.body, expires_at=excluded.expires_at", kind, key, b, until)
	return err
}
func (s *records) del(ctx context.Context, kind, key string) error {
	_, err := s.tx.Exec(ctx, "DELETE FROM envy_auth_records WHERE kind=$1 AND key=$2", kind, key)
	return err
}

// Auth mutations serialize across replicas. Responses are buffered until commit;
// replay revocations commit even when the OAuth response is an error.
func transaction(ctx context.Context, pool *pgxpool.Pool, fn func(*records) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(818821)"); err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, "DELETE FROM envy_auth_records WHERE expires_at<now()"); err != nil {
		return err
	}

	if err = fn(&records{tx}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type oauthRecord struct {
	Request json.RawMessage `json:"request"`
	Active  bool            `json:"active"`
}
type oauthStore struct {
	*records
	server *Server
}

func (s *oauthStore) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var c fosite.DefaultClient
	err := s.get(ctx, "client", id, &c)
	return &c, err
}
func (s *oauthStore) ClientAssertionJWTValid(context.Context, string) error {
	return fosite.ErrInvalidClient
}
func (s *oauthStore) SetClientAssertionJWT(context.Context, string, time.Time) error {
	return fosite.ErrInvalidClient
}
func (s *oauthStore) save(ctx context.Context, kind, key string, r fosite.Requester) error {
	// Fosite sanitizes request forms; store no bearer tokens or code verifiers.
	v := r.Sanitize([]string{"redirect_uri", "code_challenge", "code_challenge_method"})
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	sess := r.GetSession().(*oauthSession)
	if err := s.put(ctx, "grant", r.GetID(), sess, sess.Until); err != nil {
		return err
	}

	return s.put(ctx, kind, digest(key), oauthRecord{b, true}, sess.Until)
}
func (s *oauthStore) read(ctx context.Context, kind, key string) (fosite.Requester, error) {
	var v oauthRecord
	if err := s.get(ctx, kind, digest(key), &v); err != nil {
		return nil, err
	}

	r := &fosite.Request{Client: &fosite.DefaultClient{}, Session: &oauthSession{}}
	if err := json.Unmarshal(v.Request, r); err != nil {
		return nil, err
	}

	sess := r.Session.(*oauthSession)
	if !s.server.valid(ctx, s.records, sess.Identity) || !time.Now().Before(sess.Until) {
		return nil, fosite.ErrNotFound
	}

	var revoked bool
	err := s.get(ctx, "revoked", r.GetID(), &revoked)
	if err != nil && !errors.Is(err, fosite.ErrNotFound) {
		return nil, err
	}

	if revoked {
		return nil, fosite.ErrNotFound
	}

	if !v.Active {
		if kind == "code" {
			return r, fosite.ErrInvalidatedAuthorizeCode
		}

		return r, fosite.ErrInactiveToken
	}

	return r, nil
}
func (s *oauthStore) inactive(ctx context.Context, kind, key string) error {
	var v oauthRecord
	if err := s.get(ctx, kind, digest(key), &v); err != nil {
		return err
	}

	v.Active = false
	return s.put(ctx, kind, digest(key), v, time.Now().Add(31*24*time.Hour))
}
func (s *oauthStore) CreateAuthorizeCodeSession(c context.Context, k string, r fosite.Requester) error {
	return s.save(c, "code", k, r)
}
func (s *oauthStore) GetAuthorizeCodeSession(c context.Context, k string, _ fosite.Session) (fosite.Requester, error) {
	return s.read(c, "code", k)
}
func (s *oauthStore) InvalidateAuthorizeCodeSession(c context.Context, k string) error {
	return s.inactive(c, "code", k)
}
func (s *oauthStore) CreateAccessTokenSession(c context.Context, k string, r fosite.Requester) error {
	return s.save(c, "access", k, r)
}
func (s *oauthStore) GetAccessTokenSession(c context.Context, k string, _ fosite.Session) (fosite.Requester, error) {
	return s.read(c, "access", k)
}
func (s *oauthStore) DeleteAccessTokenSession(c context.Context, k string) error {
	return s.del(c, "access", digest(k))
}
func (s *oauthStore) CreateRefreshTokenSession(c context.Context, k, a string, r fosite.Requester) error {
	return s.save(c, "refresh", k, r)
}
func (s *oauthStore) GetRefreshTokenSession(c context.Context, k string, _ fosite.Session) (fosite.Requester, error) {
	return s.read(c, "refresh", k)
}
func (s *oauthStore) DeleteRefreshTokenSession(c context.Context, k string) error {
	return s.inactive(c, "refresh", k)
}
func (s *oauthStore) RotateRefreshToken(c context.Context, id, k string) error {
	return s.inactive(c, "refresh", k)
}
func (s *oauthStore) RevokeRefreshToken(c context.Context, id string) error {
	return s.put(c, "revoked", id, true, time.Now().Add(31*24*time.Hour))
}
func (s *oauthStore) RevokeAccessToken(c context.Context, id string) error {
	return s.RevokeRefreshToken(c, id)
}
func (s *oauthStore) CreatePKCERequestSession(c context.Context, k string, r fosite.Requester) error {
	return s.save(c, "pkce", k, r)
}
func (s *oauthStore) GetPKCERequestSession(c context.Context, k string, _ fosite.Session) (fosite.Requester, error) {
	return s.read(c, "pkce", k)
}
func (s *oauthStore) DeletePKCERequestSession(c context.Context, k string) error {
	return s.del(c, "pkce", digest(k))
}
