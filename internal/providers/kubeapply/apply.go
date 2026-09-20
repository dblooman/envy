// Package kubeapply implements guarded, non-forcing server-side apply payloads.
package kubeapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RuntimeManager = "envy-runtime"
	RouteManager   = "envy-routing"
	fingerprint    = "envy.dev/managed-fields-hash"
)

type Object interface{ metav1.Object }

func desired(obj Object) map[string]any {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	var data map[string]any
	if err = json.Unmarshal(b, &data); err != nil {
		panic(err)
	}

	delete(data, "status")
	delete(data, "apiVersion")
	delete(data, "kind")
	if _, ok := obj.(*corev1.Namespace); ok {
		delete(data, "spec")
	}

	meta := map[string]any{"name": obj.GetName()}
	if obj.GetNamespace() != "" {
		meta["namespace"] = obj.GetNamespace()
	}

	if len(obj.GetLabels()) > 0 {
		meta["labels"] = obj.GetLabels()
	}

	a := map[string]string{}
	for k, v := range obj.GetAnnotations() {
		if k != fingerprint {
			a[k] = v
		}
	}

	if len(a) > 0 {
		meta["annotations"] = a
	}

	data["metadata"] = meta
	// Normalize maps through JSON for recursive comparison.
	b, _ = json.Marshal(data)
	_ = json.Unmarshal(b, &data)
	return data
}

func hash(obj Object) string {
	b, _ := json.Marshal(desired(obj))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func Stamp(obj Object) {
	a := obj.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}

	a[fingerprint] = hash(obj)
	obj.SetAnnotations(a)
}

func Changed(want, got Object) bool {
	return got.GetAnnotations()[fingerprint] != hash(want) || !subset(desired(want), desired(got), "")
}

func subset(want, got any, field string) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}

		for k, v := range w {
			if !subset(v, g[k], k) {
				return false
			}
		}

		return true
	case []any:
		g, ok := got.([]any)
		if !ok {
			return len(w) == 0 && got == nil
		}

		return subsetList(w, g, field)

	default:
		return reflect.DeepEqual(want, got)
	}
}

func associativeList(values []any, field string) bool {
	switch field {
	case "containers", "initContainers", "env", "volumeMounts", "volumes", "imagePullSecrets", "ports":
	default:
		return false
	}

	for _, v := range values {
		m, ok := v.(map[string]any)
		if !ok || m["name"] == nil {
			return false
		}
	}

	return true
}

func namedSubset(w map[string]any, candidates []any, field string) bool {
	for _, candidate := range candidates {
		m, ok := candidate.(map[string]any)
		if ok && m["name"] == w["name"] && subset(w, m, field) {
			return true
		}
	}

	return false
}

func subsetList(w, g []any, field string) bool {
	if associativeList(w, field) {
		for _, v := range w {
			if !namedSubset(v.(map[string]any), g, field) {
				return false
			}
		}

		return true
	}

	if len(w) != len(g) {
		return false
	}

	for i := range w {
		if !subset(w[i], g[i], field) {
			return false
		}
	}

	return true
}

func Data(want, got Object, apiVersion, kind string) ([]byte, error) {
	Stamp(want)
	d := desired(want)
	d["apiVersion"] = apiVersion
	d["kind"] = kind
	m := d["metadata"].(map[string]any)
	m["resourceVersion"] = got.GetResourceVersion()
	m["uid"] = got.GetUID()
	a, ok := m["annotations"].(map[string]any)
	if !ok {
		a = map[string]any{}
	}

	a[fingerprint] = want.GetAnnotations()[fingerprint]
	m["annotations"] = a
	return json.Marshal(d)
}

// Conflict retains the Kubernetes status error for retry classification.
type Conflict struct {
	Resource string
	Err      error
}

func (e *Conflict) Error() string {
	return fmt.Sprintf("apply conflict on %s: %v; resolve field ownership before retrying (Envy does not force ownership)", e.Resource, e.Err)
}
func (e *Conflict) Unwrap() error              { return e.Err }
func (e *Conflict) ReconciliationCode() string { return "apply_conflict" }
func Wrap(resource string, err error) error {
	var status apierrors.APIStatus
	if apierrors.IsConflict(err) && errors.As(err, &status) {
		details := status.Status().Details
		if details != nil {
			for _, cause := range details.Causes {
				if cause.Type == metav1.CauseTypeFieldManagerConflict {
					return &Conflict{Resource: resource, Err: err}
				}
			}
		}
	}

	return err
}

// Client is the common typed Kubernetes Patch API.
type Client[T Object] interface {
	Patch(context.Context, string, types.PatchType, []byte, metav1.PatchOptions, ...string) (T, error)
}

// Apply converts only this controller's Update manager before switching to
// Apply. It never removes another controller's entry or forces ownership.
func Apply[T Object](ctx context.Context, api Client[T], want, got T, version, kind, manager string, guard func(context.Context) error) (T, error) {
	var zero T
	retained, handoff, err := handoffFields(got.GetManagedFields(), version, kind, manager)
	if err != nil {
		return zero, err
	}

	if handoff {
		var ops []map[string]any
		if got.GetResourceVersion() != "" {
			ops = append(ops, map[string]any{"op": "test", "path": "/metadata/resourceVersion", "value": got.GetResourceVersion()})
		}

		if got.GetUID() != "" {
			ops = append(ops, map[string]any{"op": "test", "path": "/metadata/uid", "value": got.GetUID()})
		}

		ops = append(ops, map[string]any{"op": "replace", "path": "/metadata/managedFields", "value": retained})
		data, err := json.Marshal(ops)
		if err != nil {
			return zero, err
		}

		if err = guard(ctx); err != nil {
			return zero, err
		}

		updated, err := api.Patch(ctx, want.GetName(), types.JSONPatchType, data, metav1.PatchOptions{})
		if err != nil {
			return zero, Wrap(kind+"/"+want.GetName(), err)
		}

		got = updated
	}

	data, err := Data(want, got, version, kind)
	if err != nil {
		return zero, err
	}

	if err = guard(ctx); err != nil {
		return zero, err
	}

	updated, err := api.Patch(ctx, want.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager})
	return updated, Wrap(kind+"/"+want.GetName(), err)
}

func mergeFields(dst, src map[string]any) {
	for key, value := range src {
		if child, ok := value.(map[string]any); ok {
			existing, ok := dst[key].(map[string]any)
			if !ok {
				existing = map[string]any{}
				dst[key] = existing
			}

			mergeFields(existing, child)
		} else {
			dst[key] = value
		}
	}
}

// Allocated Service fields and Namespace finalizers remain with their original
// manager. They are never part of Envy's declarative configuration.
func partitionFields(fields map[string]any, kind string) map[string]any {
	remainder := map[string]any{}
	if kind == "Namespace" {
		moveField(fields, remainder, "f:spec")
	}

	if kind == "Service" {
		if spec, ok := fields["f:spec"].(map[string]any); ok {
			keep := map[string]any{}
			for _, key := range []string{"f:clusterIP", "f:clusterIPs", "f:ipFamilies", "f:ipFamilyPolicy", "f:healthCheckNodePort"} {
				moveField(spec, keep, key)
			}

			if len(keep) > 0 {
				remainder["f:spec"] = keep
			}

			if len(spec) == 0 {
				delete(fields, "f:spec")
			}
		}
	}

	moveField(fields, remainder, "f:status")
	return remainder
}

func moveField(from, to map[string]any, key string) {
	if value, ok := from[key]; ok {
		to[key] = value
		delete(from, key)
	}
}

func handoffFields(entries []metav1.ManagedFieldsEntry, version, kind, manager string) ([]metav1.ManagedFieldsEntry, bool, error) {
	var retained []metav1.ManagedFieldsEntry
	merged := map[string]any{}
	handoff := false
	for _, entry := range entries {
		own := entry.Manager == manager || entry.Manager == "envy-server"
		if !own || entry.Subresource != "" || entry.APIVersion != version || entry.FieldsV1 == nil {
			retained = append(retained, entry)
			continue
		}

		var fields map[string]any
		if err := json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields); err != nil {
			return nil, false, err
		}

		remainder := partitionFields(fields, kind)
		if len(remainder) > 0 {
			raw, _ := json.Marshal(remainder)
			rest := entry
			rest.FieldsV1 = metav1.NewFieldsV1(string(raw))
			retained = append(retained, rest)
		}

		mergeFields(merged, fields)
		if len(fields) > 0 && (entry.Operation == metav1.ManagedFieldsOperationUpdate || entry.Manager != manager) {
			handoff = true
		}
	}

	fields, err := json.Marshal(merged)
	if err != nil {
		return nil, false, err
	}

	retained = append(retained, metav1.ManagedFieldsEntry{Manager: manager, Operation: metav1.ManagedFieldsOperationApply, APIVersion: version, FieldsType: "FieldsV1", FieldsV1: metav1.NewFieldsV1(string(fields))})
	return retained, handoff, nil
}
