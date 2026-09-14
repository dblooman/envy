package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	ciliumprovider "github.com/dblooman/envy/internal/providers/cilium"
	"github.com/dblooman/envy/internal/providers/gatewayapi"
	istioprovider "github.com/dblooman/envy/internal/providers/istio"
	kubeprovider "github.com/dblooman/envy/internal/providers/kubernetes"
	linkerdprovider "github.com/dblooman/envy/internal/providers/linkerd"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/spf13/cobra"
)

// InstallationCheck is deliberately read-only. Unknown means that a laptop
// check cannot establish a controller-pod prerequisite.
type InstallationCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type installationSpec struct {
	InstallationID string                  `json:"installation_id,omitempty"`
	Catalog        *domain.CatalogManifest `json:"catalog,omitempty"`
	Mesh           mesh.Config             `json:"mesh"`
	GatewayClass   string                  `json:"gateway_class,omitempty"`
	Kubeconfig     string                  `json:"kubeconfig,omitempty"`
	Namespace      string                  `json:"namespace"`
	Gateway        struct {
		Namespace      string `json:"namespace"`
		Name           string `json:"name"`
		SectionName    string `json:"section_name,omitempty"`
		RouteNamespace string `json:"route_namespace,omitempty"`
	} `json:"gateway"`
	DatabaseSecret struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"database_secret"`
	PreviewBaseURL  string            `json:"preview_base_url"`
	IngressURL      string            `json:"ingress_url"`
	BaselineHost    string            `json:"baseline_host"`
	InjectionLabels map[string]string `json:"injection_labels"`
	IngressSelector map[string]string `json:"ingress_selector"`
}

func installationCommand(r *runner) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "installation", Short: "Check an existing-cluster installation", SilenceErrors: true, SilenceUsage: true}
	check := &cobra.Command{Use: "check", Short: "Run read-only installation preflight", Args: noArgs(), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(file) == "" {
			return domain.Validation("--file is required")
		}
		spec, err := readInstallationSpec(file)
		if err != nil {
			return err
		}
		checks := evaluateInstallationSpec(spec)
		config, err := kubeConfig(spec.Kubeconfig, r.getenv("KUBECONFIG"))
		if err != nil {
			checks = append(checks, InstallationCheck{Name: "kubernetes_connection", Status: "fail", Message: err.Error()})
		} else {
			kube, e := kubernetes.NewForConfig(config)
			if e != nil {
				checks = append(checks, InstallationCheck{Name: "kubernetes_connection", Status: "fail", Message: e.Error()})
			} else {
				checks = append(checks, inspectInstallation(cmd.Context(), kube, config, spec)...)
			}
		}
		r.exitCode = installationExitCode(checks)
		r.result = map[string]any{"checks": checks, "ready": r.exitCode == 0}
		return nil
	}}
	check.Flags().StringVar(&file, "file", "", "installation JSON specification")
	cmd.AddCommand(check)
	return cmd
}

// Failure takes precedence over incomplete evidence.
func installationExitCode(checks []InstallationCheck) int {
	code := 0
	for _, check := range checks {
		if check.Status == "fail" {
			return 1
		}
		if check.Status != "pass" {
			code = 2
		}
	}
	return code
}

func readInstallationSpec(path string) (installationSpec, error) {
	var spec installationSpec
	f, err := os.Open(path)
	if err != nil {
		return spec, fmt.Errorf("read installation file: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&spec); err != nil {
		return spec, domain.Validation("invalid installation JSON: " + err.Error())
	}
	if spec.InstallationID == "" {
		spec.InstallationID = "envy-local"
	}
	profile, err := mesh.Resolve(spec.Mesh.Provider)
	if err != nil {
		return spec, domain.Validation(err.Error())
	}
	spec.Mesh.Provider = profile.Name
	spec.IngressURL, err = profile.IngressURL(spec.IngressURL)
	if err != nil {
		return spec, domain.Validation(err.Error())
	}
	if spec.GatewayClass == "" {
		spec.GatewayClass = profile.GatewayClass
	}
	return spec, nil
}

func evaluateInstallationSpec(s installationSpec) []InstallationCheck {
	checks := []InstallationCheck{}
	profile, err := mesh.Resolve(s.Mesh.Provider)
	if err != nil {
		return []InstallationCheck{{"mesh_provider", "fail", err.Error()}}
	}
	if _, err = profile.IngressURL(s.IngressURL); err != nil {
		checks = append(checks, InstallationCheck{"ingress_url", "fail", err.Error()})
	}
	if strings.TrimSpace(s.Namespace) == "" {
		checks = append(checks, InstallationCheck{"namespace", "fail", "namespace is required"})
	} else {
		checks = append(checks, InstallationCheck{"namespace", "pass", "namespace configured"})
	}
	if s.Gateway.Namespace == "" || s.Gateway.Name == "" {
		checks = append(checks, InstallationCheck{"gateway", "fail", "gateway namespace and name are required"})
	}
	if s.DatabaseSecret.Name == "" || s.DatabaseSecret.Key == "" {
		checks = append(checks, InstallationCheck{"database_secret", "fail", "database_secret name and key are required"})
	}
	for _, item := range []struct{ name, value string }{{"preview_base_url", s.PreviewBaseURL}, {"ingress_url", s.IngressURL}} {
		u, err := url.Parse(item.value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			checks = append(checks, InstallationCheck{item.name, "fail", "must be an absolute URL"})
		} else {
			checks = append(checks, InstallationCheck{item.name, "pass", "URL configured"})
		}
	}
	if strings.TrimSpace(s.BaselineHost) == "" {
		checks = append(checks, InstallationCheck{"baseline_host", "fail", "baseline_host is required"})
	}
	if profile.Name == "istio" && len(s.InjectionLabels) == 0 {
		checks = append(checks, InstallationCheck{"injection_labels", "fail", "at least one injection label is required"})
	}
	if profile.Name == "istio" && len(s.IngressSelector) == 0 {
		checks = append(checks, InstallationCheck{"ingress_selector", "fail", "at least one Gateway selector label is required"})
	}
	checks = append(checks, InstallationCheck{"controller_network", "unknown", "run the in-cluster connectivity Job before treating ingress and PostgreSQL reachability as verified"})
	return checks
}

func kubeConfig(explicit, environment string) (*rest.Config, error) {
	if explicit != "" {
		return clientcmd.BuildConfigFromFlags("", explicit)
	}
	if environment != "" {
		return clientcmd.BuildConfigFromFlags("", environment)
	}
	return rest.InClusterConfig()
}

func inspectInstallation(ctx context.Context, kube kubernetes.Interface, config *rest.Config, s installationSpec) []InstallationCheck {
	checks := []InstallationCheck{}
	if _, err := kube.CoreV1().Namespaces().Get(ctx, s.Namespace, metav1.GetOptions{}); err != nil {
		checks = append(checks, InstallationCheck{"kubernetes_namespace", "fail", "cannot read namespace: " + err.Error()})
	} else {
		checks = append(checks, InstallationCheck{"kubernetes_namespace", "pass", "namespace is readable"})
	}
	secret, err := kube.CoreV1().Secrets(s.Namespace).Get(ctx, s.DatabaseSecret.Name, metav1.GetOptions{})
	if err != nil {
		checks = append(checks, InstallationCheck{"database_secret", "fail", "cannot read referenced Secret metadata: " + err.Error()})
	} else if _, exists := secret.Data[s.DatabaseSecret.Key]; !exists {
		checks = append(checks, InstallationCheck{"database_secret", "fail", "referenced Secret does not contain the configured key"})
	} else {
		checks = append(checks, InstallationCheck{"database_secret", "pass", "referenced Secret key is readable"})
	}
	profile, err := mesh.Resolve(s.Mesh.Provider)
	if err != nil {
		return append(checks, InstallationCheck{"mesh_provider", "fail", err.Error()})
	}
	if profile.Name != "istio" {
		client, err := gatewayclient.NewForConfig(config)
		if err != nil {
			return append(checks, InstallationCheck{"gateway_api", "fail", err.Error()})
		}
		provider := gatewayapi.NewProfile(client, s.InstallationID, nil, s.GatewayClass, profile).WithKubernetes(kube)
		routeNS := s.Gateway.RouteNamespace
		if routeNS == "" {
			routeNS = s.Gateway.Namespace
		}
		err = provider.CheckGateway(ctx, s.Gateway.Namespace, s.Gateway.Name, s.Gateway.SectionName, routeNS)
		status, message := "pass", "Gateway and controller are ready"
		if err != nil {
			status, message = "fail", err.Error()
		}
		checks = append(checks, InstallationCheck{"gateway", status, message})
		if _, err := client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
			checks = append(checks, InstallationCheck{"httproutes", "fail", err.Error()})
		}
		if _, err := client.GatewayV1beta1().ReferenceGrants("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
			checks = append(checks, InstallationCheck{"referencegrants", "fail", err.Error()})
		}
		if profile.Name == "cilium" {
			err = ciliumprovider.New(client, kube, s.InstallationID, nil, s.GatewayClass).CheckPrerequisites(ctx)
		} else {
			var dyn dynamic.Interface
			dyn, err = dynamic.NewForConfig(config)
			if err == nil {
				_, err = dyn.Resource(schema.GroupVersionResource{Group: "linkerd.io", Version: "v1alpha2", Resource: "serviceprofiles"}).List(ctx, metav1.ListOptions{Limit: 1})
			}
		}
		status, message = "pass", "mesh prerequisites readable"
		if err != nil {
			status, message = "fail", err.Error()
		}
		checks = append(checks, InstallationCheck{"mesh_prerequisites", status, message})
		if s.Catalog == nil {
			checks = append(checks, InstallationCheck{"baseline", "unknown", "include catalog in the installation specification or run catalog validate to verify baseline pods and routing"})
		} else {
			components := map[string]domain.Component{}
			for _, c := range s.Catalog.Components {
				components[c.ID] = c
			}
			kv := kubeprovider.NewWithInjection(kube, s.InstallationID, nil, map[string]string{}).WithMesh(profile.Name)
			err = kv.ValidateBaseline(ctx, s.Catalog.Baseline, components)
			if err == nil {
				if profile.Name == "linkerd" {
					dyn, e := dynamic.NewForConfig(config)
					err = e
					if err == nil {
						err = linkerdprovider.New(client, kube, dyn, s.InstallationID, nil, s.GatewayClass).ValidateBaseline(ctx, s.Catalog.Baseline, components)
					}
				} else {
					dyn, e := dynamic.NewForConfig(config)
					err = e
					if err == nil {
						err = ciliumprovider.New(client, kube, s.InstallationID, nil, s.GatewayClass).WithEndpointClient(dyn).ValidateBaseline(ctx, s.Catalog.Baseline, components)
					}
				}
			}
			status, message = "pass", "baseline mesh participation and routing validated"
			if err != nil {
				status, message = "fail", err.Error()
			}
			checks = append(checks, InstallationCheck{"baseline", status, message})
		}

		return checks
	}
	istio, err := istioclient.NewForConfig(config)
	if err != nil {
		return append(checks, InstallationCheck{"istio_gateway", "fail", "create Istio client: " + err.Error()})
	}
	gateway, err := istio.NetworkingV1().Gateways(s.Gateway.Namespace).Get(ctx, s.Gateway.Name, metav1.GetOptions{})
	if err != nil {
		checks = append(checks, InstallationCheck{"istio_gateway", "fail", "cannot read configured Gateway: " + err.Error()})
	} else {
		checks = append(checks, InstallationCheck{"istio_gateway", "pass", "configured Gateway is readable"})
		if maps.Equal(gateway.Spec.Selector, s.IngressSelector) {
			checks = append(checks, InstallationCheck{"ingress_selector", "pass", "Gateway selector matches configuration"})
		} else {
			checks = append(checks, InstallationCheck{"ingress_selector", "fail", "Gateway selector does not match configuration"})
		}
	}
	if s.Catalog == nil {
		checks = append(checks, InstallationCheck{"baseline", "unknown", "include catalog in the installation specification to validate baseline participation and routing"})
	} else {
		components := map[string]domain.Component{}
		for _, c := range s.Catalog.Components {
			components[c.ID] = c
		}
		err = kubeprovider.NewWithInjection(kube, s.InstallationID, nil, s.InjectionLabels).ValidateBaseline(ctx, s.Catalog.Baseline, components)
		if err == nil {
			err = istioprovider.NewWithIngressSelector(istio, s.InstallationID, nil, s.IngressSelector).ValidateBaseline(ctx, s.Catalog.Baseline, components)
		}
		status, message := "pass", "baseline Istio participation and routing validated"
		if err != nil {
			status, message = "fail", err.Error()
		}
		checks = append(checks, InstallationCheck{"baseline", status, message})
	}
	return checks
}
