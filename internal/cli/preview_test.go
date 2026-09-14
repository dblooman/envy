package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/dblooman/envy/internal/domain"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewProfileCLI(t *testing.T) {
	approval := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(approval, []byte(`{"inspection":"fingerprint","expected_revision":0,"confirm_connectivity":true,"selection":{"deployment":"pricing"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.URL.Path, "/baselines/staging/components/pricing/preview-profile") {
			t.Errorf("scope lost: %s", r.URL.Path)
		}
		if strings.HasSuffix(r.URL.Path, "/approve") {
			var a domain.PreviewApproval
			json.NewDecoder(r.Body).Decode(&a)
			if a.Inspection != "fingerprint" || !a.ConfirmConnectivity {
				t.Error("approval lost")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"revision": 1})
	}))
	defer server.Close()
	env := func(key string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[key]
	}
	for _, action := range []string{"discover", "inspect", "approve"} {
		args := []string{"preview-profile", action, "--project", "shop", "--baseline", "staging", "--component", "pricing"}
		if action == "approve" {
			args = append(args, "--file", approval)
		}
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, env); code != 0 {
			t.Fatalf("%s: %s", action, &diag)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}
