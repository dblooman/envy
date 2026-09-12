package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

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
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Namespace  string `json:"namespace"`
	Gateway    struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"gateway"`
	DatabaseSecret struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"database_secret"`
	PreviewBaseURL string `json:"preview_base_url"`
	IngressURL     string `json:"ingress_url"`
	BaselineHost   string `json:"baseline_host"`
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
		failed := false
		for _, item := range checks {
			failed = failed || item.Status == "fail"
		}
		r.result = map[string]any{"checks": checks, "ready": !failed}
		if failed {
			r.exitCode = 1
		}
		return nil
	}}
	check.Flags().StringVar(&file, "file", "", "installation JSON specification")
	cmd.AddCommand(check)
	return cmd
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
	return spec, nil
}

func evaluateInstallationSpec(s installationSpec) []InstallationCheck {
	checks := []InstallationCheck{}
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
	if _, err := kube.CoreV1().Secrets(s.Namespace).Get(ctx, s.DatabaseSecret.Name, metav1.GetOptions{}); err != nil {
		checks = append(checks, InstallationCheck{"database_secret", "fail", "cannot read referenced Secret metadata: " + err.Error()})
	} else {
		checks = append(checks, InstallationCheck{"database_secret", "pass", "referenced Secret is readable"})
	}
	istio, err := istioclient.NewForConfig(config)
	if err != nil {
		return append(checks, InstallationCheck{"istio_gateway", "fail", "create Istio client: " + err.Error()})
	}
	if _, err = istio.NetworkingV1().Gateways(s.Gateway.Namespace).Get(ctx, s.Gateway.Name, metav1.GetOptions{}); err != nil {
		checks = append(checks, InstallationCheck{"istio_gateway", "fail", "cannot read configured Gateway: " + err.Error()})
	} else {
		checks = append(checks, InstallationCheck{"istio_gateway", "pass", "configured Gateway is readable"})
	}
	return checks
}
