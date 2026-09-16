package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type previewDBFake struct {
	*buildsFake
	GitHubPreviewStore
	previews                   map[string]domain.PRPreview
	policies                   []domain.PRPreviewPolicy
	events                     map[string]json.RawMessage
	compositions               map[string]domain.Composition
	creates, updates, destroys int
}

func (f *previewDBFake) PRPreview(_ context.Context, id string) (domain.PRPreview, error) {
	p, ok := f.previews[id]
	if !ok {
		return p, domain.NotFound("preview")
	}

	return p, nil
}

func (f *previewDBFake) PRPreviews(_ context.Context, _, _ string, _ int) ([]domain.PRPreview, string, error) {
	out := []domain.PRPreview{}
	for _, p := range f.previews {
		out = append(out, p)
	}

	return out, "", nil
}

func (f *previewDBFake) SavePRPreview(_ context.Context, p domain.PRPreview) (domain.PRPreview, error) {
	if f.previews[p.ID].Version != p.Version {
		return p, &domain.Error{Code: "conflict", Message: "stale"}
	}

	p.Version++
	f.previews[p.ID] = p
	return p, nil
}

func (f *previewDBFake) PreviewPolicies(context.Context) ([]domain.PRPreviewPolicy, error) {
	return f.policies, nil
}

func (f *previewDBFake) ReceiveGitHubWebhook(_ context.Context, id, _ string, b []byte) error {
	f.events[id] = b
	return nil
}

func (f *previewDBFake) Create(ctx context.Context, c domain.Composition, _, _ string, _ int) (domain.Composition, error) {
	f.creates++
	f.compositions[c.ID] = c
	claim, _ := domain.PreviewClaim(ctx)
	p := f.previews[claim.ID]
	p.CompositionID = c.ID
	f.previews[claim.ID] = p
	return c, nil
}

func (f *previewDBFake) Get(_ context.Context, id string) (domain.Composition, error) {
	return f.compositions[id], nil
}

func (f *previewDBFake) Update(_ context.Context, id string, in domain.UpdateRequest, _ string) (domain.Composition, error) {
	f.updates++
	c := f.compositions[id]
	c.Generation++
	c.Overrides = in.Overrides
	c.Phase = domain.PhaseUpdating
	f.compositions[id] = c
	return c, nil
}

func (f *previewDBFake) Destroy(_ context.Context, id string) (domain.Composition, error) {
	f.destroys++
	c := f.compositions[id]
	c.Phase = domain.PhaseDestroyed
	c.DeletionRequested = true
	f.compositions[id] = c
	return c, nil
}

type previewGHFake struct {
	*sourceFake
	domain.GitHubPreviewProvider
	pr          domain.GitHubPR
	runs        []domain.GitHubRun
	err         error
	feedbackErr error
	revoked     bool
}

func (f *previewGHFake) RepositoryInstalled(context.Context, domain.SourceRepository) (bool, error) {
	return !f.revoked, nil
}

func (f *previewGHFake) PullRequest(context.Context, domain.SourceRepository, int) (domain.GitHubPR, error) {
	return f.pr, f.err
}

func (f *previewGHFake) PullRequests(context.Context, domain.SourceRepository, int) ([]domain.GitHubPR, error) {
	return []domain.GitHubPR{f.pr}, f.err
}

func (f *previewGHFake) WorkflowRuns(context.Context, domain.SourceRepository, int64, string, int) ([]domain.GitHubRun, error) {
	return f.runs, f.err
}

func (f *previewGHFake) RunAttempt(_ context.Context, _ domain.SourceRepository, id string, attempt int) (domain.GitHubRun, error) {
	for _, r := range f.runs {
		if fmt.Sprint(r.ID) == id && r.Attempt == attempt {
			return r, nil
		}
	}

	return domain.GitHubRun{}, domain.NotFound("run")
}

func (f *previewGHFake) PreviewComment(context.Context, domain.SourceRepository, int, int64, string, string) (int64, error) {
	return 1, f.feedbackErr
}

func (f *previewGHFake) PreviewDeployment(context.Context, domain.SourceRepository, string, string) (int64, error) {
	return 2, f.feedbackErr
}

func (f *previewGHFake) DeploymentStatus(context.Context, domain.SourceRepository, int64, string, string) error {
	return f.feedbackErr
}

func previewFixture(t *testing.T) (*Service, *previewDBFake, *previewGHFake, domain.PRPreview) {
	t.Helper()
	_, base, src, reg, report := buildFixture()
	db := &previewDBFake{buildsFake: base, previews: map[string]domain.PRPreview{}, events: map[string]json.RawMessage{}, compositions: map[string]domain.Composition{}}
	gh := &previewGHFake{sourceFake: src}
	gh.pr.Number = 7
	gh.pr.State = "open"
	gh.pr.Head.SHA = src.head
	gh.pr.Head.Repo.ID = 10
	gh.pr.Base.Repo.ID = 10
	gh.pr.Labels = append(gh.pr.Labels, struct {
		Name string `json:"name"`
	}{domain.PreviewLabel})
	gh.runs = []domain.GitHubRun{{ID: 123, WorkflowID: 11, Number: 1, Attempt: 1, SHA: src.head, Status: "completed", Conclusion: "success"}}
	p := domain.PRPreview{ID: "demo-backend-7", Version: 1, Number: 7, Lifecycle: 1, Requested: true, RequestedSHA: src.head, Policy: domain.PRPreviewPolicy{Project: "demo", Repository: "backend", GitHubRepositoryID: 10, Enabled: true, Baseline: "staging", Components: []string{"service-b"}, TTL: "1h", WorkflowID: 11}, Status: "waiting_build"}
	db.previews[p.ID] = p
	db.policies = []domain.PRPreviewPolicy{p.Policy}
	s := New(db, Config{SourceControl: gh, ImageRegistry: reg, GitHubWebhookSecret: strings.Repeat("s", 32)})
	if _, e := s.RecordBuild(context.Background(), "demo", "backend", report); e != nil {
		t.Fatal(e)
	}

	return s, db, gh, p
}

func tickPreview(t *testing.T, s *Service, db *previewDBFake, gh *previewGHFake, id string) domain.PRPreview {
	t.Helper()
	p := db.previews[id]
	if e := s.reconcilePR(context.Background(), db, gh, p, p.Policy); e != nil {
		t.Fatal(e)
	}

	return db.previews[id]
}

func readyPreview(db *previewDBFake, p domain.PRPreview) {
	c := db.compositions[p.CompositionID]
	c.Phase = domain.PhaseReady
	c.ObservedGeneration = c.Generation
	db.compositions[c.ID] = c
}

func TestPRPreviewLifecycle(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	p = tickPreview(t, s, db, gh, p.ID)
	if db.creates != 1 || p.CompositionID == "" {
		t.Fatal("no composition")
	}

	id := p.CompositionID
	expiry := p.ExpiresAt
	readyPreview(db, p)
	p = tickPreview(t, s, db, gh, p.ID)
	if p.DeployedSHA != gh.pr.Head.SHA {
		t.Fatal("readiness did not publish revision")
	}

	gh.pr.Head.SHA = strings.Repeat("c", 40)
	p = tickPreview(t, s, db, gh, p.ID)
	if db.updates != 0 || p.DeployedSHA == p.RequestedSHA {
		t.Fatal("deployed missing build")
	}

	gh.runs = append(gh.runs, domain.GitHubRun{ID: 124, WorkflowID: 11, Number: 2, Attempt: 1, SHA: gh.pr.Head.SHA, Status: "completed", Conclusion: "success"})
	_, e := s.RecordBuild(context.Background(), "demo", "backend", domain.BuildReport{Component: "service-b", Revision: gh.pr.Head.SHA, Image: db.repo.Images["service-b"] + "@sha256:" + strings.Repeat("d", 64), RunID: "124", Attempt: 1, BuiltAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}

	p = tickPreview(t, s, db, gh, p.ID)
	if db.updates != 1 || p.CompositionID != id || !p.ExpiresAt.Equal(expiry) {
		t.Fatal("update replaced identity or extended TTL")
	}

	readyPreview(db, p)
	p = tickPreview(t, s, db, gh, p.ID)
	if p.DeployedSHA != gh.pr.Head.SHA {
		t.Fatal("new revision not published")
	}

	gh.pr.State = "closed"
	p = tickPreview(t, s, db, gh, p.ID)
	if !p.Terminal || db.destroys != 1 {
		t.Fatal("close did not destroy")
	}

	p = tickPreview(t, s, db, gh, p.ID)
	if db.creates != 1 {
		t.Fatal("closed preview recreated")
	}

	gh.pr.State = "open"
	p = tickPreview(t, s, db, gh, p.ID)
	p = tickPreview(t, s, db, gh, p.ID)
	if db.creates != 2 || p.CompositionID == id || p.Lifecycle != 2 {
		t.Fatal("reopen did not start new lifecycle")
	}
}

func TestPRPreviewExpiryAndExplicitStopDoNotRevive(t *testing.T) {
	for _, action := range []string{"expiry", "stop"} {
		t.Run(action, func(t *testing.T) {
			s, db, gh, p := previewFixture(t)
			p = tickPreview(t, s, db, gh, p.ID)
			readyPreview(db, p)
			if action == "expiry" {
				c := db.compositions[p.CompositionID]
				c.ExpiresAt = time.Now().Add(-time.Second)
				db.compositions[c.ID] = c
			} else {
				if _, e := s.ControlPRPreview(context.Background(), p.ID, "stop"); e != nil {
					t.Fatal(e)
				}
			}

			for range 3 {
				p = tickPreview(t, s, db, gh, p.ID)
			}

			if !p.Terminal || db.creates != 1 {
				t.Fatal("terminal lifecycle revived")
			}
		})
	}
}

func TestPRPreviewForkAndIndependentEnvironments(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	gh.pr.Head.Repo.ID = 99
	p = tickPreview(t, s, db, gh, p.ID)
	if p.Status != "unsupported" || db.creates != 0 || db.destroys != 0 {
		t.Fatal("fork allocated resources")
	}
}

func TestPRPreviewSelectionRequiresCompleteTrustedRun(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	p.Policy.Components = append(p.Policy.Components, "service-a")
	selected, _, _, e := s.selectPRBuilds(context.Background(), gh, db.repo, p)
	if e != nil || selected != nil {
		t.Fatal("partial run selected")
	}

	p.Policy.Components = []string{"service-b"}
	gh.runs[0].WorkflowID = 99
	selected, _, _, e = s.selectPRBuilds(context.Background(), gh, db.repo, p)
	if e != nil || selected != nil {
		t.Fatal("untrusted workflow selected")
	}

	gh.runs[0].WorkflowID = 11
	p.SelectedSHA = p.RequestedSHA
	p.RunNumber = 2
	selected, _, _, e = s.selectPRBuilds(context.Background(), gh, db.repo, p)
	if e != nil || selected != nil {
		t.Fatal("older run regressed preview")
	}
}

func TestGitHubWebhookSignaturesAndDuplicates(t *testing.T) {
	s, db, _, _ := previewFixture(t)
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(s.cfg.GitHubWebhookSecret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	ctx := context.Background()
	for range 2 {
		if e := s.ReceiveGitHubWebhook(ctx, "delivery", "pull_request", signature, body); e != nil {
			t.Fatal(e)
		}
	}

	if len(db.events) != 1 {
		t.Fatal("duplicate delivery")
	}

	for _, sig := range []string{"", signature + "00", "sha1=" + signature} {
		if e := s.ReceiveGitHubWebhook(ctx, "bad", "pull_request", sig, body); e == nil {
			t.Fatal("invalid signature accepted")
		}
	}

	if e := s.ReceiveGitHubWebhook(ctx, "changed", "pull_request", signature, append(body, ' ')); e == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestPRPreviewRapidLabelCycleAndDelayedEvents(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	p = tickPreview(t, s, db, gh, p.ID)
	oldID := p.CompositionID
	p, err := s.ControlPRPreview(context.Background(), p.ID, "stop")
	if err != nil {
		t.Fatal(err)
	}

	event := githubPRStopEvent{PR: gh.pr, Repository: gh.pr.Base.Repo}
	event.PR.UpdatedAt = p.RestartBarrierAt.Add(time.Second)
	if err = applyPRStopEvent(context.Background(), db, event); err != nil {
		t.Fatal(err)
	}

	// GitHub already has the label again when the controller observes the PR.
	for range 3 {
		p = tickPreview(t, s, db, gh, p.ID)
	}

	if p.Lifecycle != 2 || p.CompositionID == oldID || db.creates != 2 {
		t.Fatal("label cycle did not replace stopped preview")
	}

	if err = applyPRStopEvent(context.Background(), db, event); err != nil {
		t.Fatal(err)
	}

	p = tickPreview(t, s, db, gh, p.ID)
	if p.Lifecycle != 2 || p.Terminal {
		t.Fatal("delayed duplicate event stopped new lifecycle")
	}
}

func TestPRPreviewRestartUsesNewPolicyAfterCleanup(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	p = tickPreview(t, s, db, gh, p.ID)
	db.policies[0].TTL = "30m"
	p, err := s.ControlPRPreview(context.Background(), p.ID, "restart")
	if err != nil {
		t.Fatal(err)
	}

	if p.Policy.TTL != "1h" {
		t.Fatal("old lifecycle snapshot mutated")
	}

	for range 3 {
		p = tickPreview(t, s, db, gh, p.ID)
	}

	if p.Policy.TTL != "30m" || p.Lifecycle != 2 {
		t.Fatal("restart did not snapshot new policy")
	}
}

func TestPRPreviewRolloutFailureKeepsRevisionHealthDistinct(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	p = tickPreview(t, s, db, gh, p.ID)
	c := db.compositions[p.CompositionID]
	c.Phase = domain.PhaseFailed
	db.compositions[c.ID] = c
	gh.pr.Head.SHA = strings.Repeat("c", 40)
	p = tickPreview(t, s, db, gh, p.ID)
	if p.Status != "waiting_build" || p.DeploymentStatus != "failed" || p.DeployedSHA != "" {
		t.Fatal("rollout failure was presented as healthy")
	}
}

func TestPRPreviewAccessLossVersusGitHubOutage(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		s, db, gh, p := previewFixture(t)
		p = tickPreview(t, s, db, gh, p.ID)
		gh.err = &domain.Error{Code: "unavailable", Message: "GitHub unavailable"}
		gh.revoked = revoked
		err := s.reconcilePR(context.Background(), db, gh, p, p.Policy)
		if revoked {
			if err != nil || db.destroys != 1 || !db.previews[p.ID].Terminal {
				t.Fatal("confirmed access removal did not clean up", err)
			}
		} else if err == nil || db.destroys != 0 || db.previews[p.ID].Terminal {
			t.Fatal("outage was treated as revocation")
		}
	}
}

func TestPRPreviewFeedbackFailureDoesNotBlockCleanup(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	gh.feedbackErr = fmt.Errorf("GitHub unavailable")
	p = tickPreview(t, s, db, gh, p.ID)
	if p.CompositionID == "" || p.FeedbackError == "" {
		t.Fatal("feedback failure prevented deployment or was hidden")
	}

	gh.pr.State = "closed"
	p = tickPreview(t, s, db, gh, p.ID)
	if db.destroys != 1 {
		t.Fatal("feedback failure prevented cleanup")
	}
}

func TestPRPreviewRecoversPersistedIntent(t *testing.T) {
	s, db, gh, p := previewFixture(t)
	builds, run, _, e := s.selectPRBuilds(context.Background(), gh, db.repo, p)
	if e != nil {
		t.Fatal(e)
	}

	p.Builds = builds
	p.SelectedSHA = p.RequestedSHA
	p.RunNumber = run.Number
	p.Attempt = run.Attempt
	p.Status = "deploying"
	db.previews[p.ID] = p
	p = tickPreview(t, s, db, gh, p.ID)
	if db.creates != 1 || p.CompositionID == "" {
		t.Fatal("persisted intent was stranded")
	}
}
