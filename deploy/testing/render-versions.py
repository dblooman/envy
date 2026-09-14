#!/usr/bin/env python3
"""Generate the documentation's profile matrix from the acceptance manifest."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2]
v=json.loads((root/'deploy/testing/versions.json').read_text())
start='<!-- mesh-versions:start -->';end='<!-- mesh-versions:end -->'
rows=['| Profile | Kubernetes | Mesh | Gateway API | Ingress | Acceptance |','| --- | --- | --- | --- | --- | --- |']
for provider in ['istio','cilium','linkerd']:
 ingress='Istio '+v['istio'] if provider=='istio' else 'Cilium '+v['cilium'] if provider=='cilium' else 'Envoy Gateway '+v['envoy_gateway']
 status=v.get('profile_acceptance',{}).get(provider,v['acceptance'])
 rows.append(f"| {provider} | {v['kubernetes']} | {v[provider]} | {'—' if provider=='istio' else v['gateway_api']} | {ingress} | {status} |")
block=start+'\n'+'\n'.join(rows)+'\n'+end
for rel in ['docs/mesh-installation.md','site/src/content/docs/getting-started/mesh-installation.md']:
 p=root/rel;s=p.read_text()
 if start in s:want=s[:s.index(start)]+block+s[s.index(end)+len(end):]
 else:want=s.replace('## 1. Prerequisites',block+'\n\n## 1. Prerequisites')
 if '--check' in sys.argv:
  if want!=s:raise SystemExit(f'{rel}: run python3 deploy/testing/render-versions.py')
 else:p.write_text(want)
