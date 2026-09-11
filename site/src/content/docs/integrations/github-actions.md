---
title: GitHub Actions CI Adapter
description: Automatically build container images, report digests to Envy, and generate pull request preview comments.
---

Envy integrates with **GitHub Actions** to automate preview creation for every pull request without requiring cluster administrative credentials in CI.

---

## Workflow Example: Build & Spin up Preview

Add this workflow to your repository under `.github/workflows/preview.yml`:

```yaml
name: Envy Preview Environment

on:
  pull_request:
    types: [opened, synchronize, reopened, closed]

jobs:
  preview:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - name: Checkout code
        if: github.event.action != 'closed'
        uses: actions/checkout@v4

      - name: Build and push container image
        if: github.event.action != 'closed'
        id: docker_build
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: registry.internal/shop/orders:pr-${{ github.event.pull_request.number }}

      - name: Setup Delivery CLI
        run: |
          curl -sSL https://releases.envy.dev/delivery/latest/install.sh | bash
          echo "$HOME/.envy/bin" >> $GITHUB_PATH

      - name: Find existing composition ID
        id: find_composition
        env:
          GH_TOKEN: ${{ github.token }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
        run: |
          comments="$(gh api --paginate --slurp \
            "repos/${GITHUB_REPOSITORY}/issues/${PR_NUMBER}/comments")"
          composition_id="$(jq -r '
            .[][]
            | .body
            | select(contains("<!-- envy-composition-id:"))
            | capture("<!-- envy-composition-id:(?<id>[^ ]+) -->").id
          ' <<<"$comments" | tail -n 1)"
          echo "composition_id=${composition_id:-}" >> "$GITHUB_OUTPUT"

      - name: Create or update preview
        if: github.event.action != 'closed'
        env:
          ENVY_API_URL: ${{ secrets.ENVY_API_URL }}
          ENVY_API_TOKEN: ${{ secrets.ENVY_API_TOKEN }}
          GH_TOKEN: ${{ github.token }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          COMPOSITION_ID: ${{ steps.find_composition.outputs.composition_id }}
        run: |
          image="registry.internal/shop/orders:pr-${PR_NUMBER}"
          if [ -z "$COMPOSITION_ID" ]; then
            response="$(delivery composition create \
              --project shop \
              --baseline staging \
              --name "pr-${PR_NUMBER}" \
              --override "orders=${image}" \
              --ttl 12h \
              --idempotency-key "github-pr-${PR_NUMBER}")"
            COMPOSITION_ID="$(jq -r '.id' <<<"$response")"
            test -n "$COMPOSITION_ID" && test "$COMPOSITION_ID" != "null"
            gh pr comment "$PR_NUMBER" \
              --body "<!-- envy-composition-id:${COMPOSITION_ID} --> Envy composition: \`${COMPOSITION_ID}\`"
            echo "$response"
          else
            current="$(delivery composition get "$COMPOSITION_ID")"
            generation="$(jq -r '.generation' <<<"$current")"
            delivery composition update "$COMPOSITION_ID" \
              --expected-generation "$generation" \
              --override "orders=${image}"
          fi

      - name: Destroy on PR Close
        if: github.event.action == 'closed'
        env:
          ENVY_API_URL: ${{ secrets.ENVY_API_URL }}
          ENVY_API_TOKEN: ${{ secrets.ENVY_API_TOKEN }}
          COMPOSITION_ID: ${{ steps.find_composition.outputs.composition_id }}
        run: |
          if [ -n "$COMPOSITION_ID" ]; then
            delivery composition destroy "$COMPOSITION_ID"
          else
            echo "No Envy composition ID was recorded on the pull request."
          fi
```

---

## How It Works

1. **Secure Scoped Tokens**: GitHub Actions only needs an Envy API bearer token. It has no access to the Kubernetes kubeconfig or underlying node instances.
2. **Deterministic expiry**: If a PR is abandoned without closing, Envy's TTL automatically cleans up the composition after 12 hours.
3. **Stable identity**: Composition names are display metadata, not unique
   identifiers. The workflow records the returned composition ID in a hidden
   pull-request comment, then uses that ID for updates and cleanup. The stable
   idempotency key makes retries of the initial create safe.
