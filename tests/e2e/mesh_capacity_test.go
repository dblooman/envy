//go:build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"
)

func TestMeshCapacity(t *testing.T) {
	h := newHarness(t)
	ids := []string{}
	t.Cleanup(func() {
		for _, id := range ids {
			h.request("DELETE", "/v1/compositions/"+id, nil, "")
		}
		for _, id := range ids {
			h.wait(id, "destroyed")
		}
	})
	for i := 0; i < 20; i++ {
		c := h.create(fmt.Sprintf("capacity-%d", i), "envy/service-b:v2", "20m", fmt.Sprintf("capacity-%d-%d", time.Now().UnixNano(), i))
		ids = append(ids, c.ID)
	}
	for _, id := range ids {
		c := h.wait(id, "ready")
		if _, err := h.chain(c.Endpoints["public"].URL, id, "v2", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.chain(h.preview, "", "v1", ""); err != nil {
		t.Fatal(err)
	}
}
