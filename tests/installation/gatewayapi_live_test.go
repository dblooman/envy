package installation

import (
	"context"
	"fmt"
	"github.com/dblooman/envy/internal/domain"
	gatewayprovider "github.com/dblooman/envy/internal/providers/gatewayapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"net/http"
	"os"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type routeWriteCounter struct {
	next   http.RoundTripper
	writes *atomic.Int64
}

func (c routeWriteCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/httproutes/") {
		c.writes.Add(1)
	}
	return c.next.RoundTrip(r)
}

// TestGatewayAPIResources checks Kubernetes schema admission, not data-plane routing.
// It never selects a developer context implicitly and explicit runs never skip failures.
func TestGatewayAPIResources(t *testing.T) {
	if os.Getenv("ENVY_TEST_GATEWAY_API_RESOURCES") != "1" {
		t.Skip("set ENVY_TEST_GATEWAY_API_RESOURCES=1 for explicit resource integration test")
	}
	contextName := os.Getenv("ENVY_KUBE_CONTEXT")
	if contextName == "" {
		t.Fatal("ENVY_KUBE_CONTEXT is required")
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int64
	cfg.WrapTransport = func(next http.RoundTripper) http.RoundTripper { return routeWriteCounter{next, &writes} }
	cfg.Timeout = 10 * time.Second
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	gw, err := gatewayclient.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ns := "envy-schema-baseline-" + suffix
	id := "schema-" + suffix
	preview := domain.NamespaceForID(id)
	for _, namespace := range []string{ns, preview} {
		if _, err = kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			clean, done := context.WithTimeout(context.Background(), 30*time.Second)
			defer done()
			if err := kube.CoreV1().Namespaces().Delete(clean, namespace, metav1.DeleteOptions{}); err != nil {
				t.Error(err)
			}
		})
	}
	p := gatewayprovider.New(gw, "schema-test", func(context.Context) error { return nil })
	e := domain.RouteEntry{Domain: domain.RouteDomain{Namespace: ns, Gateway: "dummy", ServiceHost: "api." + ns + ".svc.cluster.local", Port: 8080, AggregateName: "envy-api"}, CompositionID: id, OwnershipToken: "token-" + id, DestinationHost: "api." + preview + ".svc.cluster.local", Host: "cmp-" + id + ".example.test", Port: 8080}
	s := domain.RouteSnapshot{MeshEntries: []domain.RouteEntry{e}, IngressEntries: []domain.RouteEntry{e}, OwnedCompositions: map[string]string{id: e.OwnershipToken}}
	obs, err := p.Reconcile(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Ready {
		t.Fatal("missing Gateway/controller must not be ready")
	}
	routes, err := gw.GatewayV1().HTTPRoutes(ns).List(ctx, metav1.ListOptions{})
	if err != nil || len(routes.Items) != 3 {
		t.Fatalf("expected 3 admitted routes: %v", err)
	}
	// Spec generation must survive API defaulting without needless writes.
	writes.Store(0)
	versions := map[string]int64{}
	for _, route := range routes.Items {
		versions[route.Name] = route.Generation
	}
	if _, err = p.Reconcile(ctx, s); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 0 {
		t.Fatal("unchanged API-defaulted routes were rewritten")
	}
	again, err := gw.GatewayV1().HTTPRoutes(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range again.Items {
		if versions[route.Name] != route.Generation {
			t.Fatalf("unchanged defaulted route changed generation: %s", route.Name)
		}
	}
	grants, err := gw.GatewayV1beta1().ReferenceGrants(preview).List(ctx, metav1.ListOptions{})
	if err != nil || len(grants.Items) != 1 || grants.Items[0].Spec.To[0].Name == nil {
		t.Fatalf("expected restricted grant: %v", err)
	}
	s.MeshEntries = nil
	s.IngressEntries = nil
	if _, err = p.Reconcile(ctx, s); err != nil {
		t.Fatal(err)
	}
	routes, err = gw.GatewayV1().HTTPRoutes(ns).List(ctx, metav1.ListOptions{})
	if err != nil || len(routes.Items) != 0 {
		t.Fatalf("stale routes: %v", err)
	}
}
