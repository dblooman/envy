package application

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
)

// A stop event is evidence of a label cycle even if the label was re-added
// before the next live-state fetch. Its GitHub timestamp fences delayed events;
// deployment still requires fetching the current PR in reconcilePR/deployPR.
type githubPRStopEvent struct {
	Event      string                  `json:"_envy_event"`
	Action     string                  `json:"action"`
	PR         domain.GitHubPR         `json:"pull_request"`
	Repository domain.GitHubRepository `json:"repository"`
	Label      struct {
		Name string `json:"name"`
	} `json:"label"`
	Sender struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"sender"`
}

func (s *Service) applyPRStopEvents(ctx context.Context, db GitHubPreviewStore, events map[string]json.RawMessage) error {
	for _, body := range events {
		var event githubPRStopEvent
		if json.Unmarshal(body, &event) != nil || event.Event != "pull_request" {
			continue
		}

		if event.Action != "closed" && (event.Action != "unlabeled" || event.Label.Name != domain.PreviewLabel) {
			continue
		}

		if e := applyPRStopEvent(ctx, db, event); e != nil {
			return e
		}
	}

	return nil
}

func applyPRStopEvent(ctx context.Context, db GitHubPreviewStore, event githubPRStopEvent) error {
	after := ""
	for {
		rows, next, e := db.PRPreviews(ctx, "", after, 100)
		if e != nil {
			return e
		}

		for _, p := range rows {
			if !matchesPRStopEvent(p, event) {
				continue
			}

			p.LastStopEventAt = event.PR.UpdatedAt
			p.Requested = false
			p.Terminal = true
			p.Status = "closed"
			p.Reason = "PR closed or preview label removed"
			actor := domain.WithRequestIdentity(ctx, domain.RequestIdentity{Principal: domain.Principal{Kind: "github", ID: strconv.FormatInt(event.Sender.ID, 10), DisplayName: event.Sender.Login}, Channel: "github"})
			if _, e = db.SavePRPreview(actor, p); e != nil {
				return e
			}
		}

		if next == "" {
			return nil
		}

		after = next
	}
}

func matchesPRStopEvent(p domain.PRPreview, event githubPRStopEvent) bool {
	return p.Policy.GitHubRepositoryID == event.Repository.ID && p.Number == event.PR.Number &&
		event.PR.UpdatedAt.After(p.RestartBarrierAt) && event.PR.UpdatedAt.After(p.LastStopEventAt)
}
