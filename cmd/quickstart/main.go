// Quickstart is the in-cluster setup Job for the evaluation Helm chart.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// Helm may create sample pods before the injector webhook is registered.
	// Wait for the real webhook, then replace only uninjected quickstart pods.
	if err = until(ctx, func() error { return injectorReady(ctx, kube) }); err != nil {
		return err
	}

	for _, item := range []struct{ namespace, name string }{{"envy-quickstart", "envy-ingress"}, {"envy-quickstart-shop", "storefront"}, {"envy-quickstart-shop", "pricing"}} {
		if err := waitDeployment(ctx, kube, item.namespace, item.name); err != nil {
			return err
		}
	}

	return registerCatalog(ctx)
}

func injectorReady(ctx context.Context, kube kubernetes.Interface) error {
	hook, err := kube.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, "istio-sidecar-injector-envy-quickstart", metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(hook.Webhooks) == 0 || len(hook.Webhooks[0].ClientConfig.CABundle) == 0 {
		return fmt.Errorf("waiting for Istio injector certificate")
	}

	endpoints, err := kube.CoreV1().Endpoints("envy-quickstart").Get(ctx, "istiod", metav1.GetOptions{})
	if err != nil {
		return err
	}

	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) > 0 {
			return nil
		}
	}

	return fmt.Errorf("waiting for istiod")
}

func waitDeployment(ctx context.Context, kube kubernetes.Interface, namespace, name string) error {
	restarted := false
	return until(ctx, func() error {
		d, err := kube.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
		if err != nil {
			return err
		}

		pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return err
		}

		injected, needsRestart := injectionState(pods.Items)
		if needsRestart {
			if !restarted {
				patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"envy.dev/injector-ready": time.Now().UTC().Format(time.RFC3339Nano)}}}}})
				if _, err := kube.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
					return err
				}

				restarted = true
			}

			return fmt.Errorf("waiting for injected %s", name)
		}

		if !injected || d.Status.ObservedGeneration < d.Generation || d.Status.ReadyReplicas < 1 || d.Status.UpdatedReplicas < 1 {
			return fmt.Errorf("waiting for %s readiness", name)
		}

		return nil
	})
}

func injectionState(pods []corev1.Pod) (injected, needsRestart bool) {
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}

		if !injectedPod(pod) {
			return false, true
		}

		injected = true
	}

	return injected, false
}

func registerCatalog(ctx context.Context) error {
	data, err := os.ReadFile("/catalog/catalog.json")
	if err != nil {
		return err
	}

	var catalog domain.CatalogManifest
	if err = json.Unmarshal(data, &catalog); err != nil {
		return err
	}

	token, err := os.ReadFile("/credentials/token")
	if err != nil {
		return err
	}

	api, err := client.NewWithIdentity("http://envy-envy.envy-quickstart.svc.cluster.local:8081", string(token), nil, "cli", "quickstart-onboarding")
	if err != nil {
		return err
	}

	if err = until(ctx, func() error { _, err := api.Onboard(ctx, catalog, true); return err }); err != nil {
		return err
	}

	log.Print("Tea shop registered. Open the dashboard to create a pricing preview.")
	return nil
}

func injectedPod(pod corev1.Pod) bool {
	if pod.Annotations["sidecar.istio.io/status"] == "" {
		return false
	}

	for _, containers := range [][]corev1.Container{pod.Spec.Containers, pod.Spec.InitContainers} {
		for _, c := range containers {
			if c.Name == "istio-proxy" && c.Image == "auto" {
				return false
			}
		}
	}

	return true
}

func until(ctx context.Context, check func() error) error {
	var last error
	for {
		if last = check(); last == nil {
			return nil
		}

		log.Printf("Waiting: %v", last)
		select {
		case <-ctx.Done():
			return fmt.Errorf("quickstart timed out: %w (last check: %w)", ctx.Err(), last)
		case <-time.After(3 * time.Second):
		}
	}
}
