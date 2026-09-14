package installation

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	gatewayprovider "github.com/dblooman/envy/internal/providers/gatewayapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

func TestGatewayAPILiveCluster(t *testing.T) {
	contextName := os.Getenv("ENVY_KUBE_CONTEXT")
	if contextName == "" {
		contextName = "rancher-desktop"
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	kubeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	if err != nil {
		t.Skipf("cannot load kubeconfig for context %q: %v", contextName, err)
	}
	kubeConfig.Timeout = 10 * time.Second

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		t.Skipf("cannot create kubernetes client: %v", err)
	}

	gwClient, err := gatewayclient.NewForConfig(kubeConfig)
	if err != nil {
		t.Skipf("cannot create gateway client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Verify connectivity and Gateway API CRDs exist
	if _, err := gwClient.GatewayV1().HTTPRoutes("default").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		t.Skipf("cluster context %q does not have Gateway API HTTPRoute CRD available: %v", contextName, err)
	}

	suffix := fmt.Sprintf("gw-%d", time.Now().UnixNano()%1000000)
	baseNS := "envy-base-" + suffix
	cmpID := "live-" + suffix
	cmpNS := domain.NamespaceForID(cmpID)

	// Clean up namespaces on exit
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_ = kubeClient.CoreV1().Namespaces().Delete(cleanCtx, baseNS, metav1.DeleteOptions{})
		_ = kubeClient.CoreV1().Namespaces().Delete(cleanCtx, cmpNS, metav1.DeleteOptions{})
	})

	// 1. Create baseline and composition namespaces
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: baseNS},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create baseline namespace %s: %v", baseNS, err)
	}

	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: cmpNS},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create composition namespace %s: %v", cmpNS, err)
	}

	// 2. Create baseline Service "service-b"
	_, err = kubeClient.CoreV1().Services(baseNS).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "service-b", Namespace: baseNS},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 8080, Name: "http"}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create baseline service-b: %v", err)
	}

	// 3. Create preview Service "service-b" in composition namespace
	_, err = kubeClient.CoreV1().Services(cmpNS).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "service-b", Namespace: cmpNS},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 8080, Name: "http"}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create preview service-b: %v", err)
	}

	// 4. Create Gateway in baseline namespace
	gwName := "test-gw-" + suffix
	_, err = gwClient.GatewayV1().Gateways(baseNS).Create(ctx, &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: baseNS},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dummy-class",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: ptrToHostname("*.envy.localhost"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create gateway: %v", err)
	}

	provider := gatewayprovider.New(gwClient, "test-install", func(context.Context) error { return nil })

	// 5. Test ValidateBaseline
	baseline := domain.Baseline{
		ID:       "shop",
		Endpoint: "http://baseline.envy.localhost",
		Routing: domain.BaselineRouting{
			Namespace: baseNS,
			Gateway:   gwName,
		},
	}
	components := map[string]domain.Component{
		"service-b": {Port: 8080},
	}

	if err := provider.ValidateBaseline(ctx, baseline, components); err != nil {
		t.Fatalf("ValidateBaseline failed: %v", err)
	}

	// 6. Test Reconcile: create preview route and mesh route
	entry := domain.RouteEntry{
		Domain: domain.RouteDomain{
			Namespace:     baseNS,
			Gateway:       gwName,
			ServiceHost:   "service-b." + baseNS + ".svc.cluster.local",
			Port:          8080,
			AggregateName: "envy-service-b",
		},
		CompositionID:   cmpID,
		Host:            "cmp-" + cmpID + ".envy.localhost",
		DestinationHost: "service-b." + cmpNS + ".svc.cluster.local",
		Port:            8080,
		OwnershipToken:  "token-" + cmpID,
	}

	snapshot := domain.RouteSnapshot{
		MeshEntries:    []domain.RouteEntry{entry},
		IngressEntries: []domain.RouteEntry{entry},
		OwnedCompositions: map[string]string{
			cmpID: "token-" + cmpID,
		},
	}

	obs, err := provider.Reconcile(ctx, snapshot)
	if err != nil {
		t.Fatalf("Reconcile preview failed: %v", err)
	}
	if !obs.Ready {
		t.Fatalf("expected RouteObservation.Ready=true, got %v (%s)", obs.Ready, obs.Message)
	}

	// 7. Inspect real live HTTPRoutes in baseline namespace
	meshRoute, err := gwClient.GatewayV1().HTTPRoutes(baseNS).Get(ctx, "envy-service-b", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get live mesh HTTPRoute envy-service-b: %v", err)
	}
	if len(meshRoute.Spec.ParentRefs) == 0 || string(meshRoute.Spec.ParentRefs[0].Name) != "service-b" {
		t.Fatalf("mesh HTTPRoute parentRef expected service-b, got %+v", meshRoute.Spec.ParentRefs)
	}
	if len(meshRoute.Spec.Rules) < 2 {
		t.Fatalf("mesh HTTPRoute rules expected >= 2 rules, got %+v", meshRoute.Spec.Rules)
	}
	// Check rule 0 targets preview namespace
	if string(*meshRoute.Spec.Rules[0].BackendRefs[0].Namespace) != cmpNS {
		t.Fatalf("backendRef 0 namespace expected %s, got %s", cmpNS, string(*meshRoute.Spec.Rules[0].BackendRefs[0].Namespace))
	}
	// Check rule 1 targets baseline namespace
	if string(*meshRoute.Spec.Rules[1].BackendRefs[0].Namespace) != baseNS {
		t.Fatalf("backendRef 1 namespace expected %s, got %s", baseNS, string(*meshRoute.Spec.Rules[1].BackendRefs[0].Namespace))
	}

	ingressRouteName := "envy-ingress-" + cmpID
	ingressRoute, err := gwClient.GatewayV1().HTTPRoutes(baseNS).Get(ctx, ingressRouteName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get live ingress HTTPRoute %s: %v", ingressRouteName, err)
	}
	expectedHost := "cmp-" + cmpID + ".envy.localhost"
	if len(ingressRoute.Spec.Hostnames) == 0 || string(ingressRoute.Spec.Hostnames[0]) != expectedHost {
		t.Fatalf("ingress route hostname expected %s, got %+v", expectedHost, ingressRoute.Spec.Hostnames)
	}
	if len(ingressRoute.Spec.Rules) == 0 || len(ingressRoute.Spec.Rules[0].Filters) < 2 {
		t.Fatalf("ingress route expected filters (request & response modifier), got %+v", ingressRoute.Spec.Rules)
	}

	// 8. Inspect live ReferenceGrant in preview namespace
	refGrant, err := gwClient.GatewayV1beta1().ReferenceGrants(cmpNS).Get(ctx, "envy-allow-mesh-routing", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get live ReferenceGrant: %v", err)
	}
	if len(refGrant.Spec.From) == 0 || string(refGrant.Spec.From[0].Namespace) != baseNS {
		t.Fatalf("referenceGrant from expected namespace %s, got %+v", baseNS, refGrant.Spec.From)
	}

	// 9. Reconcile with Overrides removed (teardown)
	emptySnapshot := domain.RouteSnapshot{
		MeshEntries:    nil,
		IngressEntries: nil,
		OwnedCompositions: map[string]string{
			cmpID: "token-" + cmpID,
		},
	}

	obsClean, err := provider.Reconcile(ctx, emptySnapshot)
	if err != nil {
		t.Fatalf("Reconcile teardown failed: %v", err)
	}
	if !obsClean.Ready {
		t.Fatalf("expected RouteObservation.Ready=true after clean, got %v", obsClean)
	}

	// Verify ingress route and ReferenceGrant were deleted
	_, err = gwClient.GatewayV1().HTTPRoutes(baseNS).Get(ctx, ingressRouteName, metav1.GetOptions{})
	if err == nil {
		t.Fatalf("expected ingress HTTPRoute to be deleted from cluster")
	}

	_, err = gwClient.GatewayV1beta1().ReferenceGrants(cmpNS).Get(ctx, "envy-allow-mesh-routing", metav1.GetOptions{})
	if err == nil {
		t.Fatalf("expected ReferenceGrant to be deleted from cluster")
	}

	t.Log("Gateway API live cluster integration test passed against Rancher Desktop!")
}

func ptrToHostname(s string) *gatewayv1.Hostname {
	h := gatewayv1.Hostname(s)
	return &h
}
