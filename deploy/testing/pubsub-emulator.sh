#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
# Cloud SDK 556.0.0 emulators, pinned by manifest digest.
pubsub_image='gcr.io/google.com/cloudsdktool/google-cloud-cli@sha256:75425994dbf3b747cd4b47d382e627651ce3bbcafc16433f38e380d9ceaeadcc'
pubsub_container="$(docker run --rm -d -p 127.0.0.1::8085 "$pubsub_image" gcloud beta emulators pubsub start --host-port=0.0.0.0:8085 --project=envy-pubsub-test)"
trap 'docker rm -f "$pubsub_container" >/dev/null' EXIT
pubsub_address="$(docker port "$pubsub_container" 8085/tcp)"
for attempt in {1..30}; do
  if curl --silent --output /dev/null "http://$pubsub_address/v1/projects/envy-pubsub-test/topics"; then
    cd "$repo_root"
    ENVY_PUBSUB_TEST_EMULATOR="http://$pubsub_address" go test ./internal/providers/pubsub -count=1 -v
    exit 0
  fi
  sleep 1
done
docker logs "$pubsub_container"
echo 'Pub/Sub emulator did not become ready' >&2
exit 1
