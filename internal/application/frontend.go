package application

import (
	"context"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type frontendRepository interface {
	BindFrontend(context.Context, domain.FrontendKey, domain.BindFrontendRequest) (domain.FrontendBinding, error)
	FrontendBinding(context.Context, domain.FrontendKey) (domain.FrontendBinding, error)
	FrontendBindings(context.Context, string, string, int) ([]domain.FrontendBinding, string, error)
	PublishFrontend(context.Context, domain.FrontendKey, domain.PublishFrontendRequest) (domain.FrontendBinding, error)
	CheckFrontend(context.Context, domain.FrontendKey, domain.FrontendCheckRequest) (domain.FrontendBinding, error)
}

func (s *Service) frontendStore(k domain.FrontendKey) (frontendRepository, error) {
	if err := domain.ValidateFrontendKey(k); err != nil {
		return nil, err
	}
	r, ok := s.store.(frontendRepository)
	if !ok {
		return nil, &domain.Error{Code: "unavailable", Message: "frontend bindings are unavailable"}
	}
	return r, nil
}
func (s *Service) BindFrontend(ctx context.Context, k domain.FrontendKey, req domain.BindFrontendRequest) (domain.FrontendBindingView, error) {
	r, err := s.frontendStore(k)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	if req.Composition == "" || len(req.Composition) > 128 || !domain.PublicFrontendURL(req.Repository, false) {
		return domain.FrontendBindingView{}, domain.Validation("composition and an HTTPS repository URL without credentials, query or fragment are required")
	}
	b, err := r.BindFrontend(ctx, k, req)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	return s.frontendView(ctx, b)
}
func (s *Service) frontendView(ctx context.Context, b domain.FrontendBinding) (domain.FrontendBindingView, error) {
	c, err := s.store.Get(ctx, b.Composition)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	if c.Project != b.Project {
		return domain.FrontendBindingView{}, domain.NotFound("composition not found in project")
	}
	return domain.ViewFrontend(b, c, time.Now()), nil
}
func (s *Service) FrontendBinding(ctx context.Context, k domain.FrontendKey) (domain.FrontendBindingView, error) {
	r, err := s.frontendStore(k)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	b, err := r.FrontendBinding(ctx, k)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	return s.frontendView(ctx, b)
}
func (s *Service) ResolveFrontend(ctx context.Context, k domain.FrontendKey) (domain.FrontendResolution, error) {
	r, err := s.frontendStore(k)
	if err != nil {
		return domain.FrontendResolution{}, err
	}
	b, err := r.FrontendBinding(ctx, k)
	if err != nil {
		return domain.FrontendResolution{}, err
	}
	c, err := s.store.Get(ctx, b.Composition)
	if err != nil {
		return domain.FrontendResolution{}, err
	}
	if c.Project != k.Project {
		return domain.FrontendResolution{}, domain.NotFound("composition not found in project")
	}
	if err = domain.FrontendCompositionAvailable(c, time.Now(), true); err != nil {
		return domain.FrontendResolution{}, err
	}
	return domain.FrontendResolution{Project: k.Project, Frontend: k.Frontend, Revision: k.Revision, Composition: c.ID, CompositionGeneration: c.Generation, BindingVersion: b.Version, APIURL: c.Endpoints["public"].URL, ExpiresAt: c.ExpiresAt, VerificationLevel: c.VerificationLevel}, nil
}
func (s *Service) FrontendBindings(ctx context.Context, id, after string, limit int) ([]domain.FrontendBindingView, string, error) {
	if limit < 1 || limit > 100 || len(after) > 130 || strings.ContainsAny(after, "\r\n") {
		return nil, "", domain.Validation("invalid frontend binding page")
	}
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	r, ok := s.store.(frontendRepository)
	if !ok {
		return nil, "", &domain.Error{Code: "unavailable", Message: "frontend bindings are unavailable"}
	}
	rows, next, err := r.FrontendBindings(ctx, id, after, limit)
	if err != nil {
		return nil, "", err
	}
	views := make([]domain.FrontendBindingView, 0, len(rows))
	for _, b := range rows {
		views = append(views, domain.ViewFrontend(b, c, time.Now()))
	}
	return views, next, nil
}
func (s *Service) PublishFrontend(ctx context.Context, k domain.FrontendKey, req domain.PublishFrontendRequest) (domain.FrontendBindingView, error) {
	r, err := s.frontendStore(k)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	if req.ExpectedVersion < 1 || !domain.PublicFrontendURL(req.URL, true) {
		return domain.FrontendBindingView{}, domain.Validation("expected_version and an HTTPS frontend URL (HTTP only on loopback) without credentials, query or fragment are required")
	}
	b, err := r.PublishFrontend(ctx, k, req)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	return s.frontendView(ctx, b)
}
func (s *Service) CheckFrontend(ctx context.Context, k domain.FrontendKey, req domain.FrontendCheckRequest) (domain.FrontendBindingView, error) {
	r, err := s.frontendStore(k)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	if req.ExpectedVersion < 1 || req.CompositionGeneration < 1 || (req.Status != "passed" && req.Status != "failed") || strings.TrimSpace(req.Message) == "" || len(req.Message) > 2000 {
		return domain.FrontendBindingView{}, domain.Validation("check requires expected_version, composition_generation, passed/failed status and a message of at most 2000 bytes")
	}
	b, err := r.CheckFrontend(ctx, k, req)
	if err != nil {
		return domain.FrontendBindingView{}, err
	}
	return s.frontendView(ctx, b)
}
