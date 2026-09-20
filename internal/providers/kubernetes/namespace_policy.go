package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/providers/kubeapply"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Infrastructure rules are operator-authored Kubernetes NetworkPolicy rules.
// Their presence is not proof that the cluster's network plugin enforces them.
type NamespacePolicy struct {
	CiliumIngress bool                                    `json:"cilium_ingress,omitempty"`
	Mode          string                                  `json:"mode,omitempty"`
	Ingress       []networkingv1.NetworkPolicyIngressRule `json:"ingress,omitempty"`
	Egress        []networkingv1.NetworkPolicyEgressRule  `json:"egress,omitempty"`
	PodSecurity   struct {
		Warn    string `json:"warn,omitempty"`
		Audit   string `json:"audit,omitempty"`
		Enforce string `json:"enforce,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"pod_security"`
}

func (p NamespacePolicy) Defaults() NamespacePolicy {
	if p.PodSecurity.Warn == "" {
		p.PodSecurity.Warn = "restricted"
	}

	if p.PodSecurity.Audit == "" {
		p.PodSecurity.Audit = "restricted"
	}

	if p.PodSecurity.Version == "" {
		p.PodSecurity.Version = "v1.36"
	}

	return p
}

func (p NamespacePolicy) Validate() error {
	if p.Mode != "" && p.Mode != "legacy" && p.Mode != "isolated" {
		return fmt.Errorf("namespace_policy.mode must be legacy or isolated")
	}

	p = p.Defaults()
	for _, level := range []string{p.PodSecurity.Warn, p.PodSecurity.Audit, p.PodSecurity.Enforce} {
		if level != "" && level != "privileged" && level != "baseline" && level != "restricted" {
			return fmt.Errorf("invalid pod security level")
		}
	}

	if !regexp.MustCompile(`^v1\.[0-9]+$`).MatchString(p.PodSecurity.Version) {
		return fmt.Errorf("pod security version must be pinned, for example v1.36")
	}

	return nil
}

func (p NamespacePolicy) Ready() error {
	if p.Mode == "isolated" && (len(p.Ingress) == 0 || len(p.Egress) == 0) {
		return fmt.Errorf("isolated namespace policy requires explicit infrastructure ingress and egress allowances (DNS, mesh, ingress and verification)")
	}

	return nil
}

func (p NamespacePolicy) Fingerprint() string {
	p = p.Defaults()
	p.Mode = ""
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (p NamespacePolicy) Labels() map[string]string {
	p = p.Defaults()
	out := map[string]string{}
	for mode, level := range map[string]string{"warn": p.PodSecurity.Warn, "audit": p.PodSecurity.Audit, "enforce": p.PodSecurity.Enforce} {
		if level != "" {
			out["pod-security.kubernetes.io/"+mode] = level
			out["pod-security.kubernetes.io/"+mode+"-version"] = p.PodSecurity.Version
		}
	}

	return out
}

func (p *Provider) WithNamespacePolicy(policy NamespacePolicy) *Provider {
	p.namespacePolicy = policy.Defaults()
	return p
}

func (p *Provider) ensureNetworkPolicy(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	if p.namespacePolicy.Mode != "isolated" {
		return nil
	}

	if err := p.namespacePolicy.Ready(); err != nil {
		return err
	}

	if s.BaselineNamespace == "" {
		return fmt.Errorf("isolated preview requires a baseline namespace")
	}

	peers := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{}},
		{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": s.BaselineNamespace}}},
	}
	want := &networkingv1.NetworkPolicy{ObjectMeta: p.sharedMetadata(s, "envy-isolation", ns), Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		Ingress: append([]networkingv1.NetworkPolicyIngressRule{{From: peers}}, p.namespacePolicy.Ingress...),
		Egress:  append([]networkingv1.NetworkPolicyEgressRule{{To: peers}}, p.namespacePolicy.Egress...),
	}}
	api := p.client.NetworkingV1().NetworkPolicies(ns)
	got, err := unchanged(p, want, func() (*networkingv1.NetworkPolicy, error) { return api.Get(ctx, want.Name, metav1.GetOptions{}) })
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		kubeapply.Stamp(want)
		_, err = api.Create(ctx, want, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
		return err
	}

	if err != nil {
		return err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}

	if !kubeapply.Changed(want, got) {
		return nil
	}

	if err = p.writable(ctx); err != nil {
		return err
	}

	_, err = kubeapply.Apply(ctx, api, want, got, "networking.k8s.io/v1", "NetworkPolicy", kubeapply.RuntimeManager, p.writable)
	return err
}
