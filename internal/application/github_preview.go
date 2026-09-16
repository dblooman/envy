package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type GitHubPreviewStore interface {
	PreviewPolicies(context.Context) ([]domain.PRPreviewPolicy, error)
	SavePreviewPolicy(context.Context, domain.PRPreviewPolicy) error
	PRPreviews(context.Context, string, string, int) ([]domain.PRPreview, string, error)
	PRPreview(context.Context, string) (domain.PRPreview, error)
	SavePRPreview(context.Context, domain.PRPreview) (domain.PRPreview, error)
	ReceiveGitHubWebhook(context.Context, string, string, []byte) error
	PendingGitHubEvents(context.Context) (map[string]json.RawMessage, error)
	FinishGitHubEvent(context.Context, string, string) error
	GitHubHealth(context.Context) (map[string]any, error)
	SetGitHubHealth(context.Context, string) error
}

func (s *Service) github() (GitHubPreviewStore, domain.GitHubPreviewProvider, error) {
	db, ok := s.store.(GitHubPreviewStore)
	gh, yes := s.cfg.SourceControl.(domain.GitHubPreviewProvider)
	if !ok || !yes {
		return nil, nil, &domain.Error{Code: "unavailable", Message: "GitHub App is not configured"}
	}

	return db, gh, nil
}

func (s *Service) WakeGitHub() {
	select {
	case s.githubWake <- struct{}{}:
	default:
	}
}

func (s *Service) GitHubStatus(ctx context.Context) (map[string]any, error) {
	db, ok := s.store.(GitHubPreviewStore)
	if !ok {
		return map[string]any{"configured": false}, nil
	}

	out, e := db.GitHubHealth(ctx)
	if e != nil {
		return nil, e
	}

	out["configured"] = s.cfg.SourceControl != nil
	out["webhook_configured"] = s.cfg.GitHubWebhookSecret != ""
	return out, nil
}

func (s *Service) GitHubInstallations(ctx context.Context, page int) ([]domain.GitHubInstallation, error) {
	if page < 1 || page > 10000 {
		return nil, domain.Validation("page must be between 1 and 10000")
	}

	_, gh, e := s.github()
	if e != nil {
		return nil, e
	}

	return gh.Installations(ctx, page)
}

func (s *Service) GitHubRepositories(ctx context.Context, id int64, page int) ([]domain.GitHubRepository, error) {
	if id < 1 || page < 1 || page > 10000 {
		return nil, domain.Validation("positive installation ID and page between 1 and 10000 required")
	}

	_, gh, e := s.github()
	if e != nil {
		return nil, e
	}

	return gh.InstallationRepositories(ctx, id, page)
}

func (s *Service) PreviewPolicies(ctx context.Context) ([]domain.PRPreviewPolicy, error) {
	db, _, e := s.github()
	if e != nil {
		return nil, e
	}

	return db.PreviewPolicies(ctx)
}

func (s *Service) SavePreviewPolicy(ctx context.Context, p domain.PRPreviewPolicy) (domain.PRPreviewPolicy, error) {
	db, gh, e := s.github()
	if e != nil {
		return p, e
	}

	bs, e := s.buildStore()
	if e != nil {
		return p, e
	}

	r, e := bs.SourceRepository(ctx, p.Project, p.Repository)
	if e != nil {
		return p, e
	}

	if !p.Enabled {
		return s.disablePreviewPolicy(ctx, db, p)
	}

	if p.TTL == "" {
		p.TTL = s.cfg.DefaultTTL.String()
	}

	if e = s.validatePreviewPolicy(ctx, r, p); e != nil {
		return p, e
	}

	repo, e := gh.PreviewAccess(ctx, r)
	if e != nil {
		return p, e
	}

	p.GitHubRepositoryID = repo.ID
	sort.Strings(p.Components)
	if _, e = gh.WorkflowRuns(ctx, r, p.WorkflowID, "", 1); e != nil {
		return p, e
	}

	e = db.SavePreviewPolicy(ctx, p)
	s.WakeGitHub()
	return p, e
}

func (s *Service) validatePreviewPolicy(ctx context.Context, r domain.SourceRepository, p domain.PRPreviewPolicy) error {
	if s.cfg.GitHubWebhookSecret == "" {
		return domain.Validation("configure a webhook secret before enabling previews")
	}

	if !r.Enabled || p.WorkflowID < 1 || len(p.Components) < 1 || len(p.Components) > domain.MaxOverrides {
		return domain.Validation("policy requires an enabled repository, trusted workflow ID, and one to three components")
	}

	ttl, e := time.ParseDuration(p.TTL)
	if e != nil || ttl <= 0 || ttl > s.cfg.MaxTTL {
		return domain.Validation("preview TTL must be positive and within installation limits")
	}

	b, e := s.store.Baseline(ctx, p.Project, p.Baseline)
	if e != nil {
		return e
	}

	seen := map[string]bool{}
	for _, c := range p.Components {
		if seen[c] || r.Images[c] == "" {
			return domain.Validation("components must be distinct registered repository mappings")
		}

		if _, ok := b.Components[c]; !ok {
			return domain.Validation("component must belong to baseline")
		}

		seen[c] = true
	}

	return nil
}

func (s *Service) disablePreviewPolicy(ctx context.Context, db GitHubPreviewStore, p domain.PRPreviewPolicy) (domain.PRPreviewPolicy, error) {
	old, e := db.PreviewPolicies(ctx)
	if e != nil {
		return p, e
	}

	for _, v := range old {
		if v.Project == p.Project && v.Repository == p.Repository {
			v.Enabled = false
			e = db.SavePreviewPolicy(ctx, v)
			s.WakeGitHub()
			return v, e
		}
	}

	return p, domain.NotFound("preview policy not found")
}

func (s *Service) PRPreviews(ctx context.Context, project, after string, limit int) ([]domain.PRPreview, string, error) {
	if e := ValidatePage(after, limit); e != nil {
		return nil, "", e
	}

	db, _, e := s.github()
	if e != nil {
		return nil, "", e
	}

	return db.PRPreviews(ctx, project, after, limit)
}

func (s *Service) PRPreview(ctx context.Context, id string) (domain.PRPreview, error) {
	db, _, e := s.github()
	if e != nil {
		return domain.PRPreview{}, e
	}

	return db.PRPreview(ctx, id)
}

func (s *Service) ControlPRPreview(ctx context.Context, id, action string) (domain.PRPreview, error) {
	db, gh, e := s.github()
	if e != nil {
		return domain.PRPreview{}, e
	}

	p, e := db.PRPreview(ctx, id)
	if e != nil {
		return p, e
	}

	switch action {
	case "stop":
		p.Terminal = true
		p.RestartBarrierAt = time.Now().UTC()
		p.Status = "stopped"
		p.Reason = "Stopped explicitly"
	case "restart":
		bs, _ := s.buildStore()
		r, e := bs.SourceRepository(ctx, p.Policy.Project, p.Policy.Repository)
		if e != nil {
			return p, e
		}

		pr, e := gh.PullRequest(ctx, r, p.Number)
		if e != nil {
			return p, e
		}

		if !r.Enabled || !pr.Requested() || pr.Head.Repo.ID != pr.Base.Repo.ID {
			return p, domain.Validation("restart requires an open labelled same-repository PR")
		}

		policies, e := db.PreviewPolicies(ctx)
		if e != nil {
			return p, e
		}

		_, found := enabledPreviewPolicy(policies, p.Policy)
		if !found {
			return p, domain.Validation("preview policy is disabled")
		}

		p.Terminal = true
		p.Status = "restarting"
		p.Reason = "Waiting for previous composition cleanup"
	default:
		return p, domain.Validation("unknown preview action")
	}

	p, e = db.SavePRPreview(ctx, p)
	s.WakeGitHub()
	return p, e
}

func enabledPreviewPolicy(policies []domain.PRPreviewPolicy, current domain.PRPreviewPolicy) (domain.PRPreviewPolicy, bool) {
	for _, policy := range policies {
		if policy.Project == current.Project && policy.Repository == current.Repository && policy.Enabled {
			return policy, true
		}
	}

	return current, false
}

func (s *Service) ReceiveGitHubWebhook(ctx context.Context, id, event, signature string, body []byte) error {
	if s.cfg.GitHubWebhookSecret == "" {
		return &domain.Error{Code: "unavailable", Message: "GitHub webhooks are not configured"}
	}

	mac := hmac.New(sha256.New, []byte(s.cfg.GitHubWebhookSecret))
	mac.Write(body)
	got, e := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if e != nil || !strings.HasPrefix(signature, "sha256=") || !hmac.Equal(got, mac.Sum(nil)) {
		return &domain.Error{Code: "unauthorized", Message: "invalid GitHub webhook signature"}
	}

	if id == "" || len(id) > 128 || !json.Valid(body) {
		return domain.Validation("valid delivery ID and JSON payload required")
	}

	db, _, e := s.github()
	if e != nil {
		return e
	}

	e = db.ReceiveGitHubWebhook(ctx, id, event, body)
	if e == nil {
		s.WakeGitHub()
	}

	return e
}

// RunGitHub runs only within the existing reconciler leadership lease. Events
// are wakeups; live PR state and durable stop decisions remain authoritative.
func (s *Service) RunGitHub(ctx context.Context, guard func(context.Context) error) {
	db, gh, e := s.github()
	if e != nil {
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	nextScan := time.Time{}
	for ctx.Err() == nil {
		if e = guard(ctx); e != nil {
			return
		}

		work := domain.WithRequestIdentity(ctx, domain.RequestIdentity{Principal: domain.Principal{Kind: "system", ID: "github-preview-controller", DisplayName: "GitHub preview controller"}, Channel: "github"})
		events, err := s.processGitHubEvents(work, db)

		scan := time.Now().After(nextScan) || len(events) > 0
		if err == nil {
			err = s.reconcileGitHub(work, db, gh, scan, guard)
		}

		reason := ""
		if err != nil {
			reason = "GitHub preview reconciliation failed; check App access, API availability and server logs"
			slog.Warn("GitHub preview reconciliation", "error", err)
		} else if scan {
			nextScan = time.Now().Add(5 * time.Minute)
		}

		if healthErr := db.SetGitHubHealth(work, reason); healthErr != nil {
			slog.Warn("persist GitHub health", "error", healthErr)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.githubWake:
			nextScan = time.Time{}
		}
	}
}

func (s *Service) processGitHubEvents(ctx context.Context, db GitHubPreviewStore) (map[string]json.RawMessage, error) {
	events, err := db.PendingGitHubEvents(ctx)
	if err != nil {
		return events, err
	}

	if err = s.applyAccessEvents(ctx, db, events); err == nil {
		err = s.applyPRStopEvents(ctx, db, events)
	}

	reason := ""
	if err != nil {
		reason = "Could not persist GitHub lifecycle decisions; will retry"
	}

	// Once event decisions are durable, recovery scans own deployment retries.
	// An unavailable repository must not pin the delivery queue indefinitely.
	for id := range events {
		if finishErr := db.FinishGitHubEvent(ctx, id, reason); finishErr != nil {
			err = errors.Join(err, finishErr)
		}
	}

	return events, err
}

func (s *Service) reconcileGitHub(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, scan bool, guard func(context.Context) error) error {
	policies, e := db.PreviewPolicies(ctx)
	if e != nil {
		return e
	}

	byKey := map[string]domain.PRPreviewPolicy{}
	var errs []error
	for _, policy := range policies {
		byKey[policy.Project+"/"+policy.Repository] = policy
		if !scan || !policy.Enabled {
			continue
		}

		errs = append(errs, s.discoverPRPreviews(ctx, db, gh, policy))
	}

	after := ""
	for {
		rows, next, e := db.PRPreviews(ctx, "", after, 100)
		if e != nil {
			return e
		}

		for _, p := range rows {
			if e = guard(ctx); e != nil {
				return e
			}

			policy, ok := byKey[p.Policy.Project+"/"+p.Policy.Repository]
			if !ok {
				policy = p.Policy
				policy.Enabled = false
			}

			if e = s.reconcilePR(ctx, db, gh, p, policy); e != nil {
				errs = append(errs, e)
			}
		}

		if next == "" {
			break
		}

		after = next
	}

	return errors.Join(errs...)
}

func (s *Service) discoverPRPreviews(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, policy domain.PRPreviewPolicy) error {
	bs, _ := s.buildStore()
	r, e := bs.SourceRepository(ctx, policy.Project, policy.Repository)
	if e != nil || !r.Enabled {
		return e
	}

	for page := 1; ; page++ {
		prs, e := gh.PullRequests(ctx, r, page)
		if e != nil {
			return e
		}

		for _, pr := range prs {
			if !pr.Requested() {
				continue
			}

			if e = discoverPRPreview(ctx, db, policy, pr); e != nil {
				return e
			}
		}

		if len(prs) < 30 {
			return nil
		}
	}
}

func discoverPRPreview(ctx context.Context, db GitHubPreviewStore, policy domain.PRPreviewPolicy, pr domain.GitHubPR) error {
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%d", policy.Project, policy.Repository, policy.GitHubRepositoryID, pr.Number)))
	id := hex.EncodeToString(identity[:16])
	_, e := db.PRPreview(ctx, id)
	var de *domain.Error
	if errors.As(e, &de) && de.Code == "not_found" {
		_, e = db.SavePRPreview(ctx, domain.PRPreview{ID: id, Policy: policy, Number: pr.Number, Lifecycle: 1, Requested: true, RequestedSHA: pr.Head.SHA, PRURL: pr.URL, Status: "waiting_build", RestartBarrierAt: pr.UpdatedAt})
	}

	return e
}

func (s *Service) reconcilePR(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, p domain.PRPreview, policy domain.PRPreviewPolicy) error {
	bs, _ := s.buildStore()
	r, e := bs.SourceRepository(ctx, p.Policy.Project, p.Policy.Repository)
	if e != nil {
		return e
	}

	if e = observePreviewPR(ctx, gh, r, &p, policy); e != nil {
		return e
	}

	c, e := s.observePreviewComposition(ctx, &p)
	if e != nil {
		return e
	}

	p, e = db.SavePRPreview(ctx, p)
	if e != nil {
		return e
	}

	if p.Terminal {
		return s.finishPRPreview(ctx, db, gh, r, p, c)
	}

	return s.advancePRPreview(ctx, db, gh, r, p, c)
}

func observePreviewPR(ctx context.Context, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p *domain.PRPreview, policy domain.PRPreviewPolicy) error {
	pr, e := gh.PullRequest(ctx, r, p.Number)
	if e != nil {
		installed, accessErr := gh.RepositoryInstalled(ctx, r)
		if accessErr == nil && !installed {
			p.Terminal = true
			p.Status = "access_removed"
			p.Reason = "GitHub App access removed or suspended"
		}

		if !p.Terminal && policy.Enabled && r.Enabled {
			return e
		}
	} else {
		wasRequested := p.Requested
		p.Requested = pr.Requested()
		p.RequestedSHA = pr.Head.SHA
		p.PRURL = pr.URL
		if !p.Requested {
			p.Terminal = true
			p.Status = "closed"
			p.Reason = "PR closed or preview label removed"
		} else if !wasRequested {
			p.Terminal = true
			p.Status = "restarting"
		}

		if pr.Head.Repo.ID != pr.Base.Repo.ID {
			p.Terminal = true
			p.Status = "unsupported"
			p.Reason = "Fork PR previews are not supported"
		}
	}

	if !policy.Enabled || !r.Enabled {
		p.Terminal = true
		p.Status = "disabled"
		p.Reason = "Repository preview automation disabled"
	}

	return nil
}

func (s *Service) observePreviewComposition(ctx context.Context, p *domain.PRPreview) (domain.Composition, error) {
	var c domain.Composition
	var e error
	if p.CompositionID != "" {
		c, e = s.Get(ctx, p.CompositionID)
		if e != nil {
			return c, e
		}

		p.ExpiresAt = c.ExpiresAt
		p.Generation = c.Generation
		p.DeploymentStatus = string(c.Phase)
		if !c.ExpiresAt.After(time.Now()) && !p.Terminal {
			p.Terminal = true
			p.RestartBarrierAt = c.ExpiresAt
			p.Status = "expired"
			p.Reason = "TTL expired; restart explicitly"
		}

		if c.DeletionRequested && !p.Terminal {
			p.Terminal = true
			p.Status = "stopped"
			p.Reason = "Composition was deleted"
		}
	}

	return c, nil
}

func (s *Service) finishPRPreview(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p domain.PRPreview, c domain.Composition) error {
	if p.CompositionID != "" && c.Phase != domain.PhaseDestroyed {
		_, e := s.Destroy(domain.WithPRPreviewClaim(ctx, p), p.CompositionID)
		if e != nil {
			return e
		}

		return s.previewFeedback(ctx, db, gh, r, p)
	}

	if p.Status == "restarting" {
		policies, e := db.PreviewPolicies(ctx)
		if e != nil {
			return e
		}

		policy, enabled := enabledPreviewPolicy(policies, p.Policy)
		if !enabled {
			return domain.Validation("preview policy is disabled")
		}

		p.Policy = policy
		p.Lifecycle++
		p.RestartBarrierAt = time.Now().UTC()
		p.Terminal = false
		p.CompositionID = ""
		p.DeployedSHA = ""
		p.SelectedSHA = ""
		p.Builds = nil
		p.RunNumber = 0
		p.Attempt = 0
		p.Generation = 0
		p.DeploymentStatus = ""
		p.URL = ""
		p.Status = "waiting_build"
		p.Reason = ""
		p.ExpiresAt = time.Time{}
		if p.DeploymentID != 0 {
			p.RetiringDeploymentIDs = append(p.RetiringDeploymentIDs, p.DeploymentID)
			p.DeploymentID = 0
			p.DeploymentSHA = ""
		}

		_, e = db.SavePRPreview(ctx, p)
		return e
	}

	return s.previewFeedback(ctx, db, gh, r, p)
}

func (s *Service) advancePRPreview(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p domain.PRPreview, c domain.Composition) error {
	if p.CompositionID != "" {
		previewReadiness(&p, c)
		if c.Phase != domain.PhaseReady && c.Phase != domain.PhaseFailed {
			return s.previewFeedback(ctx, db, gh, r, p)
		}
	}

	builds, run, reason, e := s.selectPRBuilds(ctx, gh, r, p)
	if e != nil {
		return e
	}

	if builds == nil {
		p.Reason = reason
		if p.DeployedSHA != p.RequestedSHA {
			p.Status = "waiting_build"
		}

		return s.previewFeedback(ctx, db, gh, r, p)
	}

	if p.SelectedSHA == p.RequestedSHA && p.RunNumber == run.Number && p.Attempt == run.Attempt {
		if p.CompositionID == "" || !previewBuildsMatch(c.Overrides, p.Builds) {
			return s.deployPR(ctx, db, gh, r, p, c)
		}

		return s.previewFeedback(ctx, db, gh, r, p)
	}

	// Persist the exact intent before submitting it, allowing crash-safe replay.
	p.Builds = builds
	p.SelectedSHA = p.RequestedSHA
	p.RunNumber = run.Number
	p.Attempt = run.Attempt
	p.Reason = ""
	p.Status = "deploying"
	p, e = db.SavePRPreview(ctx, p)
	if e != nil {
		return e
	}

	return s.deployPR(ctx, db, gh, r, p, c)
}

func previewReadiness(p *domain.PRPreview, c domain.Composition) {
	p.Status = string(c.Phase)
	for _, endpoint := range c.Endpoints {
		if endpoint.URL != "" {
			p.URL = endpoint.URL
			break
		}
	}

	if c.Phase == domain.PhaseReady && c.ObservedGeneration == c.Generation && previewBuildsMatch(c.Overrides, p.Builds) {
		p.DeployedSHA = p.SelectedSHA
		p.Reason = ""
	}
}

// Installation removal/suspension is authoritative even when its token can no
// longer fetch the PR. Cleanup uses Envy ownership, never a GitHub credential.
type githubAccessEvent struct {
	Event        string `json:"_envy_event"`
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Removed []domain.GitHubRepository `json:"repositories_removed"`
	Sender  struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"sender"`
}

func (s *Service) applyAccessEvents(ctx context.Context, db GitHubPreviewStore, events map[string]json.RawMessage) error {
	for _, body := range events {
		var event githubAccessEvent
		if json.Unmarshal(body, &event) != nil {
			continue
		}

		if event.Event != "installation" && event.Event != "installation_repositories" {
			continue
		}

		if event.Action != "deleted" && event.Action != "suspend" && event.Action != "removed" {
			continue
		}

		if e := s.applyAccessEvent(ctx, db, event); e != nil {
			return e
		}
	}

	return nil
}

func (s *Service) applyAccessEvent(ctx context.Context, db GitHubPreviewStore, event githubAccessEvent) error {
	after := ""
	for {
		rows, next, e := db.PRPreviews(ctx, "", after, 100)
		if e != nil {
			return e
		}

		for _, p := range rows {
			if e = s.revokePreviewAccess(ctx, db, event, p); e != nil {
				return e
			}
		}

		if next == "" {
			break
		}

		after = next
	}

	return nil
}

func (s *Service) revokePreviewAccess(ctx context.Context, db GitHubPreviewStore, event githubAccessEvent, p domain.PRPreview) error {
	bs, _ := s.buildStore()
	r, e := bs.SourceRepository(ctx, p.Policy.Project, p.Policy.Repository)
	if e != nil {
		return e
	}

	if r.InstallationID != event.Installation.ID {
		return nil
	}

	removed := event.Action == "deleted" || event.Action == "suspend"
	for _, repo := range event.Removed {
		removed = removed || repo.ID == p.Policy.GitHubRepositoryID
	}

	if !removed {
		return nil
	}

	p.Terminal = true
	p.Status = "access_removed"
	p.Reason = "GitHub App access removed or suspended"
	actor := domain.WithRequestIdentity(ctx, domain.RequestIdentity{Principal: domain.Principal{Kind: "github", ID: strconv.FormatInt(event.Sender.ID, 10), DisplayName: event.Sender.Login}, Channel: "github"})
	_, e = db.SavePRPreview(actor, p)
	return e
}

func (s *Service) deployPR(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p domain.PRPreview, c domain.Composition) error {
	current, err := gh.PullRequest(ctx, r, p.Number)
	if err != nil {
		return err
	}

	if !current.Requested() || current.Head.SHA != p.SelectedSHA || current.Head.Repo.ID != current.Base.Repo.ID {
		s.WakeGitHub()
		return nil
	}

	var e error
	key := fmt.Sprintf("pr:%s:%d:%s:%d:%d", p.ID, p.Lifecycle, p.SelectedSHA, p.RunNumber, p.Attempt)
	digest := sha256.Sum256([]byte(key))
	key = hex.EncodeToString(digest[:])
	claim := domain.WithPRPreviewClaim(ctx, p)
	if p.CompositionID == "" {
		c, e = s.Create(claim, domain.CreateRequest{Project: p.Policy.Project, Baseline: p.Policy.Baseline, Name: fmt.Sprintf("pr-%d-%s", p.Number, p.Policy.Repository), Overrides: p.Builds, TTL: p.Policy.TTL}, key)
	} else {
		c, e = s.Update(claim, p.CompositionID, domain.UpdateRequest{ExpectedGeneration: c.Generation, Overrides: p.Builds, IdempotencyKey: key})
	}

	if e != nil {
		p.Reason = "Deployment request failed: " + e.Error()
		p.Status = "blocked"
		_, saveErr := db.SavePRPreview(ctx, p)
		return errors.Join(e, saveErr)
	}

	p.CompositionID = c.ID
	p.Generation = c.Generation
	p.ExpiresAt = c.ExpiresAt
	p.Status = string(c.Phase)
	p.DeploymentStatus = string(c.Phase)
	return s.previewFeedback(ctx, db, gh, r, p)
}

func previewBuildsMatch(a, b map[string]domain.ComponentOverride) bool {
	if len(a) != len(b) {
		return false
	}

	for c, v := range b {
		if a[c].BuildID != v.BuildID {
			return false
		}
	}

	return len(b) > 0
}

func previewWorkflowRuns(ctx context.Context, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p domain.PRPreview) ([]domain.GitHubRun, error) {
	var runs []domain.GitHubRun
	for page := 1; ; page++ {
		part, e := gh.WorkflowRuns(ctx, r, p.Policy.WorkflowID, p.RequestedSHA, page)
		if e != nil {
			return nil, e
		}

		runs = append(runs, part...)
		if len(part) < 30 {
			break
		}
	}

	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Number == runs[j].Number {
			return runs[i].Attempt > runs[j].Attempt
		}

		return runs[i].Number > runs[j].Number
	})
	return runs, nil
}

func (s *Service) selectPRBuilds(ctx context.Context, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p domain.PRPreview) (map[string]domain.ComponentOverride, domain.GitHubRun, string, error) {
	runs, e := previewWorkflowRuns(ctx, gh, r, p)
	if e != nil {
		return nil, domain.GitHubRun{}, "", e
	}

	for _, run := range runs {
		if !eligiblePreviewRun(run, p) {
			continue
		}

		if p.SelectedSHA == p.RequestedSHA && (run.Number < p.RunNumber || run.Number == p.RunNumber && run.Attempt < p.Attempt) {
			continue
		}

		verified, e := gh.RunAttempt(ctx, r, strconv.FormatInt(run.ID, 10), run.Attempt)
		if e != nil {
			return nil, run, "", e
		}

		if verified.ID != run.ID || verified.Attempt != run.Attempt || !eligiblePreviewRun(verified, p) {
			continue
		}

		selected, e := s.previewBuildsForRun(ctx, p, run)
		if e != nil {
			return nil, run, "", e
		}

		if len(selected) == len(p.Policy.Components) {
			return selected, run, "", nil
		}

		return nil, run, "Waiting for every configured component report from the latest successful run", nil
	}

	return nil, domain.GitHubRun{}, "Waiting for a successful trusted workflow build of the current PR head", nil
}

func eligiblePreviewRun(run domain.GitHubRun, p domain.PRPreview) bool {
	return run.WorkflowID == p.Policy.WorkflowID && run.SHA == p.RequestedSHA && run.Conclusion == "success" && run.Status == "completed"
}

func (s *Service) previewBuildsForRun(ctx context.Context, p domain.PRPreview, run domain.GitHubRun) (map[string]domain.ComponentOverride, error) {
	bs, _ := s.buildStore()
	selected := map[string]domain.ComponentOverride{}
	for _, component := range p.Policy.Components {
		after := ""
		for {
			rows, next, e := bs.Builds(ctx, p.Policy.Project, p.Policy.Repository, component, p.RequestedSHA, after, 100)
			if e != nil {
				return nil, e
			}

			for _, b := range rows {
				if b.RunID == strconv.FormatInt(run.ID, 10) && b.Attempt == run.Attempt {
					selected[component] = domain.ComponentOverride{BuildID: b.ID}
				}
			}

			if next == "" {
				break
			}

			after = next
		}
	}

	return selected, nil
}

func (s *Service) previewFeedback(ctx context.Context, db GitHubPreviewStore, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p domain.PRPreview) error {
	expiry := "TTL starts when the environment is created"
	if !p.ExpiresAt.IsZero() {
		expiry = p.ExpiresAt.Format(time.RFC3339)
	}

	body := fmt.Sprintf("### Envy preview\nStatus: **%s**\n\nEnvironment: **%s**\n\n%s\n\nRequested commit: `%s`\n\nDeployed commit: `%s`\n\nPreview: %s\n\nExpires: %s\n\nRestart from Envy’s PR previews screen or `delivery pr-preview restart %s`. Readiness does not certify application tests.", p.Status, p.DeploymentStatus, p.Reason, p.RequestedSHA, p.DeployedSHA, p.URL, expiry, p.ID)
	hash := sha256.Sum256([]byte(body))
	fingerprint := hex.EncodeToString(hash[:])
	feedbackErr := retirePreviewDeployments(ctx, gh, r, &p)
	if p.FeedbackHash != fingerprint && feedbackErr == nil {
		feedbackErr = s.syncPreviewDeployment(ctx, gh, r, &p)
		if feedbackErr == nil {
			p.CommentID, feedbackErr = gh.PreviewComment(ctx, r, p.Number, p.CommentID, "<!-- envy-preview:"+s.cfg.Installation+":"+p.ID+" -->", body)
		}

		if feedbackErr == nil {
			p.FeedbackHash = fingerprint
		}
	}

	p.FeedbackError = ""
	if feedbackErr != nil {
		p.FeedbackError = "GitHub feedback unavailable; will retry"
	}

	_, e := db.SavePRPreview(ctx, p)
	return e
}

func retirePreviewDeployments(ctx context.Context, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p *domain.PRPreview) error {
	for len(p.RetiringDeploymentIDs) > 0 {
		if e := gh.DeploymentStatus(ctx, r, p.RetiringDeploymentIDs[0], "inactive", ""); e != nil {
			return e
		}

		p.RetiringDeploymentIDs = p.RetiringDeploymentIDs[1:]
	}

	return nil
}

func (s *Service) syncPreviewDeployment(ctx context.Context, gh domain.GitHubPreviewProvider, r domain.SourceRepository, p *domain.PRPreview) error {
	var feedbackErr error
	if p.DeploymentID != 0 && (p.Terminal || p.DeploymentSHA != p.SelectedSHA) {
		feedbackErr = gh.DeploymentStatus(ctx, r, p.DeploymentID, "inactive", p.URL)
		if feedbackErr == nil {
			p.DeploymentID = 0
			p.DeploymentSHA = ""
		}
	}

	if feedbackErr == nil && !p.Terminal && p.SelectedSHA != "" && p.DeploymentID == 0 {
		p.DeploymentID, feedbackErr = gh.PreviewDeployment(ctx, r, p.SelectedSHA, fmt.Sprintf("envy-%s-%s-%d", s.cfg.Installation, p.ID, p.Lifecycle))
		if feedbackErr == nil {
			p.DeploymentSHA = p.SelectedSHA
		}
	}

	if feedbackErr == nil && p.DeploymentID != 0 {
		feedbackErr = gh.DeploymentStatus(ctx, r, p.DeploymentID, previewDeploymentState(p), p.URL)
	}

	return feedbackErr
}

func previewDeploymentState(p *domain.PRPreview) string {
	if p.DeploymentStatus == "failed" || p.Status == "blocked" {
		return "failure"
	}

	if p.DeploymentStatus == "ready" && p.DeployedSHA == p.SelectedSHA {
		return "success"
	}

	return "in_progress"
}
