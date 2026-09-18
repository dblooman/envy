---
title: Dashboard Simulation
description: Run the frontend from source with sample data and no Kubernetes cluster.
---

For frontend development, run the dashboard with sample data. You need Node
24.8+ (Node 24), pnpm 10.20.0, and a checkout of the repository.

```sh
cd web
pnpm install --frozen-lockfile
pnpm dev
```

Open the address printed by Vite, normally `http://localhost:5173`, and select
**Explore demo**. No API or Kubernetes cluster is required. Once in the workspace,
the simulation switch is under **Installation → Demo**.

Simulation does not deploy workloads or provide working preview endpoints.
See the [web interface walkthrough](/guides/web-interface/) for the sample flow,
or [run the full source stack](/getting-started/quickstart/) to develop against
a real local cluster.

To use the prebuilt server and bundled UI, follow [Install a Release](/getting-started/installation/).
