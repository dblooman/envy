package application

import (
	"context"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func (s *Service) Events(ctx context.Context, id, after string, limit int) (domain.EventsPage, error) {
	if err := ValidatePage(after, limit); err != nil {
		return domain.EventsPage{}, err
	}

	if _, err := domain.EventCursor(after); err != nil {
		return domain.EventsPage{}, err
	}

	return s.store.Events(ctx, id, after, limit)
}

func (s *Service) Logs(ctx context.Context, id, component string, options domain.LogOptions) (domain.ComponentLogs, error) {
	var zero domain.ComponentLogs
	options, err := domain.NormalizeLogOptions(options)
	if err != nil {
		return zero, err
	}

	c, err := s.store.Get(ctx, id)
	if err != nil {
		return zero, err
	}

	if c.Phase == domain.PhaseDestroyed {
		return zero, &domain.Error{Code: "conflict", Message: "logs are not retained after composition destruction", Composition: id}
	}

	if _, ok := c.Components[component]; !ok {
		return zero, domain.NotFound("component is not part of composition")
	}

	if _, err = s.store.Component(ctx, c.Project, component); err != nil {
		return zero, err
	}

	target := domain.LogTarget{Composition: id, Project: c.Project, Component: component, Source: "override", Workload: c.Runtime.WorkloadFor(component)}
	if _, overridden := c.Overrides[component]; !overridden {
		var baseline domain.Baseline
		var err error
		if c.Runtime.Plan != nil {
			baseline = c.Runtime.Plan.Baseline
		} else {
			baseline, err = s.store.Baseline(ctx, c.Project, c.Baseline)
		}

		if err != nil {
			return zero, err
		}

		binding, ok := baseline.Components[component]
		if !ok || baseline.Revision != c.BaselineRevision {
			return zero, &domain.Error{Code: "conflict", Message: "registered baseline binding is no longer available", Composition: id}
		}

		target.Source = "shared-baseline"
		target.BaselineServiceHost = binding.ServiceHost
	}

	if s.cfg.Logs == nil {
		return zero, &domain.Error{Code: "unavailable", Message: "component logs are unavailable", Retryable: true}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := s.cfg.Logs.ReadLogs(ctx, target, options)
	if err != nil {
		return zero, err
	}

	// Source labelling belongs to the application, not to individual log contents.
	result.ID = id
	result.Project = c.Project
	result.Component = component
	result.Source = target.Source
	result.CompositionFiltered = false
	result.Message = "Application container logs from composition-owned workloads; no request-level filtering."
	if target.Source == "shared-baseline" {
		result.Message = "Shared-baseline logs include baseline traffic and other compositions; not filtered to this composition."
	}

	if result.Streams == nil {
		result.Streams = []domain.LogStream{}
	}

	return result, nil
}
