#!/usr/bin/env bash
# Build the self-contained chart artifact; consumers do not run this script.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
chart="$root/deploy/helm/envy-quickstart"
helm repo add envy-istio https://blob.istio.io/istio-release/charts --force-update
helm dependency build "$chart"
# Istio templates CRDs by default, too late for custom resources in the same
# release. Ship its pinned upstream CRDs in Helm's install-first crds directory.
mkdir -p "$chart/crds"
tar -xOf "$chart/charts/base-1.31.0.tgz" base/files/crd-all.gen.yaml > "$chart/crds/istio.yaml"
helm package "$chart" --destination "${1:-$root/dist}"
