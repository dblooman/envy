"""Render the disposable composite fixture; never read cloud configuration."""

import copy
import json
import sys
from pathlib import Path


def render(root: Path, state: Path, helper_image: str) -> None:
    baseline = [
        json.loads(document)
        for document in (root / "deploy/kubernetes/baseline.yaml")
        .read_text()
        .split("---")
        if document.strip()
    ]
    deployment = next(
        obj
        for obj in baseline
        if obj["kind"] == "Deployment" and obj["metadata"]["name"] == "service-b"
    )
    spec = deployment["spec"]["template"]["spec"]
    spec["serviceAccountName"] = "baseline-app"
    spec["securityContext"]["fsGroup"] = 65532
    app = spec["containers"][0]
    app["name"] = "application"
    app["ports"] = [{"name": "application", "containerPort": 8081}]
    for probe in ("readinessProbe", "livenessProbe"):
        app[probe]["httpGet"]["port"] = "application"
    app["env"] = [
        {"name": "LISTEN_ADDR", "value": ":8081"},
        {
            "name": "FIXTURE_CONFIG",
            "valueFrom": {
                "configMapKeyRef": {"name": "composite-app", "key": "FIXTURE_CONFIG"}
            },
        },
        {
            "name": "FIXTURE_SECRET",
            "valueFrom": {
                "secretKeyRef": {"name": "composite-app", "key": "FIXTURE_SECRET"}
            },
        },
        {
            "name": "APP_MEMORY_LIMIT",
            "valueFrom": {
                "resourceFieldRef": {
                    "containerName": "application",
                    "resource": "limits.memory",
                }
            },
        },
    ]
    app["volumeMounts"] = [
        {"name": "app-config", "mountPath": "/fixture-config", "readOnly": True},
        {"name": "app-secret", "mountPath": "/fixture-secret", "readOnly": True},
        {"name": "limits", "mountPath": "/fixture-limits", "readOnly": True},
    ]
    spec["volumes"] = [
        {"name": "work", "emptyDir": {}},
        {"name": "app-config", "configMap": {"name": "composite-app"}},
        {"name": "app-secret", "secret": {"secretName": "composite-app"}},
        {
            "name": "limits",
            "downwardAPI": {
                "items": [
                    {
                        "path": "application-memory",
                        "resourceFieldRef": {
                            "containerName": "application",
                            "resource": "limits.memory",
                        },
                    },
                    {
                        "path": "proxy-memory",
                        "resourceFieldRef": {
                            "containerName": "proxy",
                            "resource": "limits.memory",
                        },
                    },
                ]
            },
        },
    ]

    def helper(name: str, mode: str, request_cpu: str, limit_cpu: str) -> dict:
        container = {
            "name": name,
            "image": helper_image,
            "imagePullPolicy": "IfNotPresent",
            "args": [mode],
            "securityContext": copy.deepcopy(app["securityContext"]),
            "resources": {
                "requests": {"cpu": request_cpu, "memory": "16Mi"},
                "limits": {"cpu": limit_cpu, "memory": "32Mi"},
            },
            "envFrom": [
                {"configMapRef": {"name": f"composite-{mode}"}},
                {"secretRef": {"name": f"composite-{mode}"}},
            ],
            "volumeMounts": [{"name": "work", "mountPath": "/fixture-work"}],
        }
        return container

    proxy = helper("proxy", "proxy", "20m", "100m")
    proxy["ports"] = [{"name": "proxy", "containerPort": 8080}]
    proxy["readinessProbe"] = {
        "httpGet": {"path": "/proxy-ready", "port": "proxy"},
        "periodSeconds": 2,
    }
    bootstrap = helper("bootstrap", "init", "250m", "600m")
    bootstrap["resources"]["requests"]["memory"] = "96Mi"
    bootstrap["resources"]["limits"]["memory"] = "192Mi"
    native = helper("native-helper", "native", "30m", "100m")
    native["restartPolicy"] = "Always"
    native["ports"] = [{"name": "helper", "containerPort": 8082}]
    native["startupProbe"] = {
        "httpGet": {"path": "/readyz", "port": "helper"},
        "periodSeconds": 1,
        "failureThreshold": 30,
    }
    native["readinessProbe"] = {
        "httpGet": {"path": "/readyz", "port": "helper"},
        "periodSeconds": 2,
    }
    # Order is deliberate: an index-zero application implementation must fail.
    spec["containers"] = [proxy, app]
    spec["initContainers"] = [bootstrap, native]
    objects = [
        {
            "apiVersion": "v1",
            "kind": "ServiceAccount",
            "metadata": {
                "name": "baseline-app",
                "namespace": "envy-baseline",
                "annotations": {"fixture.envy.dev/source-only": "not-for-copying"},
            },
            "automountServiceAccountToken": False,
        }
    ]
    names = []
    for mode in ("app", "init", "native", "proxy"):
        name = f"composite-{mode}"
        names.append(name)
        for kind, key, value in (
            ("ConfigMap", "FIXTURE_CONFIG", f"{mode}-config"),
            ("Secret", "FIXTURE_SECRET", f"synthetic-opaque-{mode}-payload"),
        ):
            obj = {
                "apiVersion": "v1",
                "kind": kind,
                "metadata": {"name": name, "namespace": "envy-baseline"},
                "stringData" if kind == "Secret" else "data": {key: value},
            }
            if kind == "Secret":
                obj["type"] = "Opaque"
            objects.append(obj)
    objects.append(deployment)
    # Catalog routing hosts have one owner. Keep the seeded demo catalog intact
    # and give the explicitly opted-in project its own source namespace/hosts.
    isolated = json.loads(
        json.dumps(baseline + objects[:-1])
        .replace("envy-baseline", "envy-composite-baseline")
        .replace("baseline.envy.localhost", "composite.envy.localhost")
    )
    objects.extend(isolated)
    (state / "composite-fixture.json").write_text(
        json.dumps({"apiVersion": "v1", "kind": "List", "items": objects})
    )

    permissions = [
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "Role",
            "metadata": {"name": "composite-source", "namespace": "envy-baseline"},
            "rules": [
                {
                    "apiGroups": [""],
                    "resources": ["secrets", "configmaps"],
                    "resourceNames": names,
                    "verbs": ["get"],
                }
            ],
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {"name": "envy-preview-dependencies"},
            "rules": [
                {
                    "apiGroups": [""],
                    "resources": ["secrets", "configmaps"],
                    "verbs": ["get", "create", "delete"],
                }
            ],
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {"name": "envy-preview-bind"},
            "rules": [
                {
                    "apiGroups": ["rbac.authorization.k8s.io"],
                    "resources": ["rolebindings"],
                    "verbs": ["get", "create"],
                },
                {
                    "apiGroups": ["rbac.authorization.k8s.io"],
                    "resources": ["clusterroles"],
                    "resourceNames": ["envy-preview-dependencies"],
                    "verbs": ["bind"],
                },
            ],
        },
    ]
    for name, role_kind, namespace in (
        ("composite-source", "Role", "envy-baseline"),
        ("envy-preview-bind", "ClusterRole", None),
    ):
        metadata = {"name": name}
        if namespace:
            metadata["namespace"] = namespace
        permissions.append(
            {
                "apiVersion": "rbac.authorization.k8s.io/v1",
                "kind": "RoleBinding" if namespace else "ClusterRoleBinding",
                "metadata": metadata,
                "roleRef": {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": role_kind,
                    "name": name,
                },
                "subjects": [
                    {
                        "kind": "ServiceAccount",
                        "name": "envy-server",
                        "namespace": "envy-system",
                    }
                ],
            }
        )
    permissions.extend(
        json.loads(
            json.dumps(
                [obj for obj in permissions if "namespace" in obj["metadata"]]
            ).replace("envy-baseline", "envy-composite-baseline")
        )
    )
    (state / "composite-permissions.json").write_text(
        json.dumps({"apiVersion": "v1", "kind": "List", "items": permissions})
    )
    # Preserve the local harness's namespace network policy.
    config_map = json.loads((state / "server-policy.json").read_text())
    config = json.loads(config_map["data"]["config.json"])
    config["preview"] = {
        "controller_namespace": "envy-system",
        "controller_service_account": "envy-server",
        "dependency_cluster_role": "envy-preview-dependencies",
        "composite": {
            "composite/staging/service-b": {
                "revision": 1,
                "application_container": "application",
                "sidecars": ["proxy"],
                "init_containers": ["bootstrap"],
                "native_sidecars": ["native-helper"],
                "source_service_account": "baseline-app",
                "service_account": "composite-workload",
                "service_account_annotations": {
                    "fixture.envy.dev/identity": "synthetic"
                },
                "shared_dependencies": [
                    "Shared synthetic database proxy; no external cloud dependencies"
                ],
                "max_pod_cpu": "1",
                "max_pod_memory": "512Mi",
            }
        },
    }
    (state / "composite-server-config.json").write_text(json.dumps(config))


if __name__ == "__main__":
    render(Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3])
