//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Probe from an actual mesh participant so unrelated baggage members reach the
// producer route unchanged, independently of ingress canonicalization tests.
func TestMixedBaggageInsideMesh(t *testing.T) {
	h := newHarness(t)
	c := h.create("mixed-baggage", "envy/service-b:v2", "10m", fmt.Sprintf("mixed-%d", time.Now().UnixNano()))
	t.Cleanup(func() { h.destroy(c.ID); h.absent(c.ID, c.Endpoints["public"].URL) })
	c = h.wait(c.ID, "ready")
	name := fmt.Sprintf("envy-baggage-%d", time.Now().UnixNano())
	data, err := os.ReadFile("../../deploy/testing/versions.json")
	if err != nil {
		t.Fatal(err)
	}
	var versions struct {
		Images map[string]string `json:"images"`
	}
	if err = json.Unmarshal(data, &versions); err != nil {
		t.Fatal(err)
	}
	probeImage := ""
	for image, digest := range versions.Images {
		if strings.HasPrefix(image, "curlimages/curl:") {
			probeImage = image + "@" + digest
		}
	}
	if probeImage == "" {
		t.Fatal("curl probe image is not pinned")
	}
	h.kubectl("run", name, "-n", "envy-baseline", "--image="+probeImage, "--restart=Never", "--command", "--", "sleep", "3600")
	t.Cleanup(func() { h.kubectl("delete", "pod", name, "-n", "envy-baseline", "--wait=false") })
	h.kubectl("wait", "pod/"+name, "-n", "envy-baseline", "--for=condition=Ready", "--timeout=180s")
	for _, tc := range []struct{ id, version string }{{c.ID, "v2"}, {"unmatched-composition", "v1"}} {
		out := h.kubectl("exec", "-n", "envy-baseline", name, "-c", name, "--", "curl", "--fail", "--silent", "--max-time", "10", "-H", "baggage: unrelated=hello", "-H", "baggage: composition="+tc.id+";debug=yes,other=value", "http://service-a.envy-baseline.svc.cluster.local:8080/")
		var response struct {
			Chain []hop `json:"chain"`
		}
		if err := json.Unmarshal([]byte(out), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Chain) != 2 || response.Chain[0].Version != "v1" || response.Chain[1].Version != tc.version {
			t.Fatalf("wrong mixed-baggage route: %s", out)
		}
		for _, hop := range response.Chain {
			if hop.Composition != tc.id {
				t.Fatalf("baggage was not propagated: %s", out)
			}
		}
	}
	h.baseline()
}
