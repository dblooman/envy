package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
)

func TestPreviewReadErrorsDistinguishPermissionFromMissingResource(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{
		{apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "api", errors.New("denied")), "permission_denied"},
		{apierrors.NewNotFound(schema.GroupResource{Resource: "deployments"}, "api"), "not_found"},
	} {
		var product *domain.Error
		if !errors.As(previewReadError("Deployment", "api", tc.cause), &product) || product.Code != tc.code {
			t.Fatalf("read error should be %s", tc.code)
		}
	}
}

func TestNamedDiscoveryPrefillsWithoutBroadReadsOrMutation(t *testing.T) {
	p, k, baseline, component := previewFixture(t)
	selection := domain.PreviewSelection{Deployment: "pricing"}
	report, err := p.DiscoverPreview(context.Background(), baseline, component, selection)
	if err != nil || len(report.Blockers) != 0 || report.Selection.Deployment != "pricing" || report.Selection.Container != "app" || report.Source.Deployment != "pricing" || len(report.Dependencies) != 2 {
		t.Fatalf("named discovery failed to prefill supported source: %+v %v", report, err)
	}

	for _, action := range k.Actions() {
		if action.GetVerb() != "get" || action.GetNamespace() != "staging" {
			t.Fatalf("discovery broadened or mutated source access: %s %s %s", action.GetVerb(), action.GetResource().Resource, action.GetNamespace())
		}
	}

	if len(report.SourceReadRules) < 2 || !strings.Contains(strings.Join(report.Warnings, " "), "Shared") {
		t.Fatalf("missing permissions or shared-dependency warning: %+v", report)
	}

	denied, deniedClient, deniedBaseline, deniedComponent := previewFixture(t)
	deniedClient.PrependReactor("get", "deployments", func(_ ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "pricing", errors.New("denied"))
	})
	deniedReport, err := denied.DiscoverPreview(context.Background(), deniedBaseline, deniedComponent, selection)
	var product *domain.Error
	if !errors.As(err, &product) || product.Code != "permission_denied" || len(deniedReport.SourceReadRules) != 2 {
		t.Fatalf("denied named read lacks a distinct permission result: %+v %v", deniedReport, err)
	}

	for _, action := range deniedClient.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("denied discovery attempted wider access: %s", action.GetVerb())
		}
	}

	unsupported, unsupportedClient, unsupportedBaseline, unsupportedComponent := previewFixture(t)
	deployment, err := unsupportedClient.AppsV1().Deployments("staging").Get(context.Background(), "pricing", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	deployment.Spec.Template.Spec.HostNetwork = true
	if _, err = unsupportedClient.AppsV1().Deployments("staging").Update(context.Background(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	unsupportedClient.ClearActions()
	unsupportedReport, err := unsupported.DiscoverPreview(context.Background(), unsupportedBaseline, unsupportedComponent, selection)
	if err != nil || !strings.Contains(strings.Join(unsupportedReport.Blockers, ";"), "unsupported Pod setting: hostNetwork") {
		t.Fatalf("unsupported Pod field was silently dropped: %+v %v", unsupportedReport, err)
	}

	for _, action := range unsupportedClient.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("unsupported shape triggered wider access or mutation: %s", action.GetVerb())
		}
	}
}
