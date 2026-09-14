#!/usr/bin/env python3
"""Fail explicit Linkerd runs early if its route status cannot prove generation."""
import json,os,pathlib,subprocess,time
state=pathlib.Path(os.environ['ENVY_STATE_DIR'])
route={'apiVersion':'gateway.networking.k8s.io/v1','kind':'HTTPRoute','metadata':{'name':'envy-generation-probe','namespace':'envy-baseline'},'spec':{'parentRefs':[{'group':'','kind':'Service','name':'service-b','port':8080}],'rules':[{'backendRefs':[{'name':'service-b','port':8080}]}]}}
subprocess.run(['kubectl','create','-f','-'],input=json.dumps(route),text=True,check=True)
try:
 for attempt in range(60):
  r=json.loads(subprocess.check_output(['kubectl','get','httproute/envy-generation-probe','-n','envy-baseline','-o','json'],text=True))
  for parent in r.get('status',{}).get('parents',[]):
   if parent.get('controllerName')!='linkerd.io/policy-controller':continue
   conditions={c['type']:c for c in parent.get('conditions',[])}
   if all(conditions.get(k,{}).get('status')=='True' for k in ['Accepted','ResolvedRefs']):
    (state/'linkerd-generation-status.json').write_text(json.dumps(r,indent=2)+'\n')
    if any(conditions[k].get('observedGeneration')!=r['metadata']['generation'] for k in ['Accepted','ResolvedRefs']):
     raise SystemExit('Linkerd profile BLOCKED: Accepted/ResolvedRefs omit the current observedGeneration; strict Envy readiness is retained')
    print('Linkerd current-generation route status verified')
    break
  else:
   time.sleep(2);continue
  break
 else:raise SystemExit('Linkerd did not accept the conformance route before timeout')
finally:
 subprocess.run(['kubectl','delete','httproute/envy-generation-probe','-n','envy-baseline','--ignore-not-found'],check=True)
