#!/usr/bin/env bash
# Called only by the disposable derived-preview acceptance harness.
set -euo pipefail
source "$(dirname "$0")/common.sh"
[[ "$ENVY_CLUSTER_NAME" == envy-derived-* ]] || { echo 'Requires a disposable envy-derived-* cluster' >&2; exit 1; }
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
curl -fsSL --retry 3 --max-time 120 https://raw.githubusercontent.com/argoproj/argo-cd/v3.1.0/manifests/install.yaml -o "$ENVY_STATE_DIR/argocd-install.yaml"
kubectl apply --server-side -n argocd -f "$ENVY_STATE_DIR/argocd-install.yaml" > "$ENVY_STATE_DIR/argocd-install.log"
python3 - "$ENVY_ROOT" "$ENVY_STATE_DIR" <<'PY'
import json,sys
from pathlib import Path
root,state=map(Path,sys.argv[1:])
objects=[json.loads(x) for x in (root/'deploy/kubernetes/baseline.yaml').read_text().split('---') if x.strip()]
for obj in objects:
 if obj['kind']=='Deployment' and obj['metadata']['name']=='service-b':
  spec=obj['spec']['template']['spec'];spec['serviceAccountName']='default'
  app=spec['containers'][0]
  app['env']=[e for e in app.get('env',[]) if e['name'] not in ('POD_UID','ENVY_COMPOSITION_ID')]
  app['env'].append({'name':'PREVIEW_FIXTURE','value':'first'})
  app['env'].append({'name':'FIXTURE_SECRET','valueFrom':{'secretKeyRef':{'name':'preview-fixture','key':'value'}}})
  app['volumeMounts']=[{'name':'settings','mountPath':'/fixture-settings','readOnly':True},{'name':'secret','mountPath':'/fixture-secret','readOnly':True}]
  spec['volumes']=[{'name':'settings','configMap':{'name':'preview-fixture'}},{'name':'secret','secret':{'secretName':'preview-fixture'}}]
objects.extend([
 {'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':'preview-fixture','namespace':'envy-baseline'},'data':{'value':'first'}},
 {'apiVersion':'v1','kind':'Secret','metadata':{'name':'preview-fixture','namespace':'envy-baseline'},'type':'Opaque','stringData':{'value':'fake-fixture-first'}}])
(state/'baseline-git.json').write_text(json.dumps({'apiVersion':'v1','kind':'List','items':objects}))
PY
kubectl -n argocd create configmap preview-git-seed --from-file=baseline.json="$ENVY_STATE_DIR/baseline-git.json" --dry-run=client -o yaml | kubectl apply -f -
cat > "$ENVY_STATE_DIR/git-server.yaml" <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata: {name: preview-git, namespace: argocd}
spec:
  replicas: 1
  selector: {matchLabels: {app: preview-git}}
  template:
    metadata: {labels: {app: preview-git}}
    spec:
      containers:
        - name: git
          image: alpine/git:2.49.1
          command: [/bin/sh, -ec]
          args:
            - |
              mkdir -p /repos/baseline
              cp /seed/baseline.json /repos/baseline/
              git -C /repos/baseline init -b main
              git -C /repos/baseline config user.email fixture@example.invalid
              git -C /repos/baseline config user.name Fixture
              git -C /repos/baseline add .
              git -C /repos/baseline commit -m baseline
              apk add --no-cache git-daemon
              exec git daemon --reuseaddr --export-all --base-path=/repos --listen=0.0.0.0 /repos
          ports: [{containerPort: 9418}]
          volumeMounts: [{name: seed, mountPath: /seed, readOnly: true}]
      volumes: [{name: seed, configMap: {name: preview-git-seed}}]
---
apiVersion: v1
kind: Service
metadata: {name: preview-git, namespace: argocd}
spec:
  selector: {app: preview-git}
  ports: [{port: 9418, targetPort: 9418}]
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: preview-baseline, namespace: argocd}
spec:
  project: default
  source:
    repoURL: git://preview-git.argocd.svc.cluster.local/baseline
    targetRevision: main
    path: .
  destination: {server: 'https://kubernetes.default.svc', namespace: envy-baseline}
  syncPolicy:
    automated: {prune: true, selfHeal: true}
    syncOptions: [FailOnSharedResource=true]
YAML
kubectl apply -f "$ENVY_STATE_DIR/git-server.yaml"
# Named source permissions only. Destination role is bound by Envy per composition.
cat > "$ENVY_STATE_DIR/preview-permissions.yaml" <<'YAML'
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: preview-source, namespace: envy-baseline}
rules:
  - apiGroups: ['']
    resources: [secrets, configmaps]
    resourceNames: [preview-fixture]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: preview-source, namespace: envy-baseline}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: preview-source}
subjects: [{kind: ServiceAccount, name: envy-server, namespace: envy-system}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: envy-preview-dependencies}
rules:
  - apiGroups: ['']
    resources: [secrets, configmaps]
    verbs: [get, create, delete]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: envy-preview-bind}
rules:
  - apiGroups: [rbac.authorization.k8s.io]
    resources: [rolebindings]
    verbs: [get, create]
  - apiGroups: [rbac.authorization.k8s.io]
    resources: [clusterroles]
    resourceNames: [envy-preview-dependencies]
    verbs: [bind]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: envy-preview-bind}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: envy-preview-bind}
subjects: [{kind: ServiceAccount, name: envy-server, namespace: envy-system}]
YAML
kubectl apply -f "$ENVY_STATE_DIR/preview-permissions.yaml"
# These environment variables configure the local-manifest controller (Helm uses config.json).
kubectl -n envy-system set env deployment/envy-server ENVY_PREVIEW_CONTROLLER_NAMESPACE=envy-system ENVY_PREVIEW_CONTROLLER_SERVICE_ACCOUNT=envy-server ENVY_PREVIEW_DEPENDENCY_CLUSTER_ROLE=envy-preview-dependencies
kubectl -n argocd rollout status deployment/preview-git --timeout=180s
kubectl -n argocd rollout status deployment/argocd-repo-server --timeout=300s
kubectl -n argocd rollout status statefulset/argocd-application-controller --timeout=300s
kubectl -n envy-system rollout status deployment/envy-server --timeout=180s
kubectl -n argocd annotate application/preview-baseline argocd.argoproj.io/refresh=hard --overwrite
kubectl -n argocd wait application/preview-baseline --for=jsonpath='{.status.health.status}'=Healthy --timeout=300s
kubectl -n argocd wait application/preview-baseline --for=jsonpath='{.status.sync.status}'=Synced --timeout=300s
