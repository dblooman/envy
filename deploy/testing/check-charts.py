#!/usr/bin/env python3
"""Render every mesh profile and verify the JSON config and RBAC boundary."""
import json, subprocess
chart='deploy/helm/envy'
for provider in ['istio','cilium','linkerd']:
 cmd=['helm','template','envy',chart,'--set','installationID=chart-test','--set','mesh.provider='+provider]
 if provider!='istio':cmd+=['--set','runtime.ingressURL=https://gateway.example.test']
 rendered=subprocess.check_output(cmd,text=True)
 assert ('resources: [virtualservices]' in rendered)==(provider=='istio')
 assert ('resources: [serviceprofiles]' in rendered)==(provider=='linkerd')
 assert 'resources: [pods]\n    verbs: [get, list, watch]' in rendered
 assert 'resources: [pods/log]\n    verbs: [get]' in rendered
 assert 'resources: [networkpolicies]' in rendered
 assert ('resources: [ciliumnetworkpolicies]' in rendered)==(provider=='cilium')
 assert 'ciliumenvoyconfigs' not in rendered
 assert 'native_cec' not in rendered and 'inject_annotation' not in rendered
 if provider!='istio':assert 'istio-ingressgateway' not in rendered
 for doc in rendered.split('---'):
  if 'config.json: |-' in doc:
   config=json.loads(doc.split('config.json: |-')[1]);assert config['mesh']['provider']==provider
   assert config['namespace_policy']['mode']==''
   assert config['namespace_policy']['pod_security']['version']=='v1.36'
   assert ('istio' in config)==(provider=='istio')
for provider in ['gateway-api','unknown']:
 assert subprocess.run(['helm','template','envy',chart,'--set','installationID=test','--set','mesh.provider='+provider],capture_output=True).returncode!=0
for provider in ['cilium','linkerd']:
 assert subprocess.run(['helm','template','envy',chart,'--set','installationID=test','--set','mesh.provider='+provider],capture_output=True).returncode!=0
print('All three mesh chart profiles and invalid configurations checked')
