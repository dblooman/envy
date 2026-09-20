package kubernetes

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/dblooman/envy/internal/providers/kubeapply"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	kube "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Observations belongs to a single leadership session. Cached objects are never
// used for mutations, and lister results are copied before leaving this adapter.
type Observations struct {
	factory informers.SharedInformerFactory
	routes  dynamicinformer.DynamicSharedInformerFactory
	mesh    string
}

func NewObservations(client kube.Interface, dyn dynamic.Interface, mesh string) *Observations {
	return &Observations{factory: informers.NewSharedInformerFactory(client, 0), routes: dynamicinformer.NewDynamicSharedInformerFactory(dyn, 0), mesh: mesh}
}

func (o *Observations) Start(ctx context.Context, notify func(string)) error {
	informers := []cache.SharedIndexInformer{o.factory.Core().V1().Namespaces().Informer(), o.factory.Core().V1().ServiceAccounts().Informer(), o.factory.Core().V1().ResourceQuotas().Informer(), o.factory.Apps().V1().Deployments().Informer(), o.factory.Core().V1().Services().Informer(), o.factory.Core().V1().Pods().Informer(), o.factory.Discovery().V1().EndpointSlices().Informer(), o.factory.Networking().V1().NetworkPolicies().Informer()}
	resources := []schema.GroupVersionResource{}
	if o.mesh == "istio" {
		resources = append(resources, schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"})
	} else {
		resources = append(resources, schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"})
	}

	if o.mesh == "cilium" {
		resources = append(resources, ciliumPolicies)
	}

	for _, gvr := range resources {
		informers = append(informers, o.routes.ForResource(gvr).Informer())
	}

	onEvent := func(obj any) { o.notifyEvent(obj, notify) }
	for _, inf := range informers {
		if _, err := inf.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: onEvent, DeleteFunc: onEvent, UpdateFunc: func(old, new any) {
			a, e := meta.Accessor(old)
			b, f := meta.Accessor(new)
			if e == nil && f == nil && a.GetResourceVersion() != b.GetResourceVersion() {
				onEvent(new)
			}
		}}); err != nil {
			return err
		}
	}

	o.factory.Start(ctx.Done())
	o.routes.Start(ctx.Done())
	syncCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	funcs := make([]cache.InformerSynced, 0, len(informers))
	for _, inf := range informers {
		funcs = append(funcs, inf.HasSynced)
	}

	if !cache.WaitForCacheSync(syncCtx.Done(), funcs...) {
		return fmt.Errorf("kubernetes observation caches did not synchronize; check list/watch permissions and API availability")
	}

	return nil
}
func (p *Provider) WithObservations(o *Observations) *Provider { p.observations = o; return p }
func (p *Provider) observeNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	if p.observations == nil {
		return p.client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	}

	v, e := p.observations.factory.Core().V1().Namespaces().Lister().Get(name)
	if e != nil {
		return nil, e
	}

	return v.DeepCopy(), nil
}

func (p *Provider) observeDeployment(ctx context.Context, ns, name string) (*appsv1.Deployment, error) {
	if p.observations == nil {
		return p.client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	}

	v, e := p.observations.factory.Apps().V1().Deployments().Lister().Deployments(ns).Get(name)
	if e != nil {
		return nil, e
	}

	return v.DeepCopy(), nil
}

func (p *Provider) observeService(ctx context.Context, ns, name string) (*corev1.Service, error) {
	if p.observations == nil {
		return p.client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	}

	v, e := p.observations.factory.Core().V1().Services().Lister().Services(ns).Get(name)
	if e != nil {
		return nil, e
	}

	return v.DeepCopy(), nil
}

func (p *Provider) observePods(ctx context.Context, ns string, selector *metav1.LabelSelector) (*corev1.PodList, error) {
	if p.observations == nil {
		return p.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(selector)})
	}

	sel, e := metav1.LabelSelectorAsSelector(selector)
	if e != nil {
		return nil, e
	}

	pods, e := p.observations.factory.Core().V1().Pods().Lister().Pods(ns).List(sel)
	if e != nil {
		return nil, e
	}

	out := &corev1.PodList{}
	for _, v := range pods {
		out.Items = append(out.Items, *v.DeepCopy())
	}

	return out, nil
}

func (p *Provider) observeEndpoints(ctx context.Context, ns, name string) (*discoveryv1.EndpointSliceList, error) {
	if p.observations == nil {
		return p.client.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + name})
	}

	slices, e := p.observations.factory.Discovery().V1().EndpointSlices().Lister().EndpointSlices(ns).List(labels.SelectorFromSet(map[string]string{"kubernetes.io/service-name": name}))
	if e != nil {
		return nil, e
	}

	out := &discoveryv1.EndpointSliceList{}
	for _, v := range slices {
		out.Items = append(out.Items, *v.DeepCopy())
	}

	return out, nil
}

// unchanged uses a cache entry only when no write would be needed. Any drift,
// absent entry or ownership mismatch falls through to a fresh API read before
// the caller can authorize a mutation.
func unchanged[T interface {
	metav1.Object
	runtime.Object
}](p *Provider, want T, get func() (T, error)) (T, error) {
	if p.observations != nil {
		f := p.observations.factory
		var obj runtime.Object
		var err error
		ns, name := want.GetNamespace(), want.GetName()
		switch any(want).(type) {
		case *corev1.Namespace:
			obj, err = f.Core().V1().Namespaces().Lister().Get(name)
		case *corev1.Service:
			obj, err = f.Core().V1().Services().Lister().Services(ns).Get(name)
		case *corev1.ServiceAccount:
			obj, err = f.Core().V1().ServiceAccounts().Lister().ServiceAccounts(ns).Get(name)
		case *corev1.ResourceQuota:
			obj, err = f.Core().V1().ResourceQuotas().Lister().ResourceQuotas(ns).Get(name)
		case *appsv1.Deployment:
			obj, err = f.Apps().V1().Deployments().Lister().Deployments(ns).Get(name)
		case *networkingv1.NetworkPolicy:
			obj, err = f.Networking().V1().NetworkPolicies().Lister().NetworkPolicies(ns).Get(name)
		}

		if err == nil && obj != nil {
			if cached, ok := obj.(T); ok && !reflect.ValueOf(cached).IsNil() && cached.GetDeletionTimestamp() == nil && p.owned(cached, want.GetAnnotations()[OwnershipAnnotation]) == nil && !kubeapply.Changed(want, cached) {
				return cached.DeepCopyObject().(T), nil
			}
		}
	}

	return get()
}

func (o *Observations) notifyEvent(obj any, notify func(string)) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}

	m, err := meta.Accessor(obj)
	if err != nil {
		return
	}

	namespace := m.GetNamespace()
	if _, ok := obj.(*corev1.Namespace); ok {
		namespace = m.GetName()
	}

	// Associate slices with their Service rather than relying on Envy labels.
	// Namespace dispatch deliberately wakes all domains sharing that baseline.
	if slice, ok := obj.(*discoveryv1.EndpointSlice); ok {
		serviceName := slice.Labels[discoveryv1.LabelServiceName]
		if serviceName == "" {
			return
		}

		service, err := o.factory.Core().V1().Services().Lister().Services(namespace).Get(serviceName)
		if err == nil {
			namespace = service.Namespace
		}

		// A concurrent Service deletion still needs to wake its routing domain.
	}

	notify(namespace)
}
