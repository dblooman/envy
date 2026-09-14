#!/usr/bin/env python3
"""Install demo workloads and the actual Envy chart in an explicit test cluster."""
import base64, json, os, pathlib, secrets, subprocess, sys, time, urllib.request
provider=sys.argv[1]
root=pathlib.Path(__file__).resolve().parents[2]
state=pathlib.Path(os.environ['ENVY_STATE_DIR'])
port=int(os.environ['ENVY_PREVIEW_PORT']); api=int(os.environ['ENVY_API_PORT'])
https=os.environ.get('ENVY_TEST_HTTPS','1')=='1'
scheme='https' if https else 'http'
versions=json.loads((root/'deploy/testing/versions.json').read_text())
def run(*args): return subprocess.check_output(args,text=True)
def apply(obj): subprocess.run(['kubectl','apply','-f','-'],input=json.dumps(obj),text=True,check=True)
def objects(path): return [json.loads(s) for s in path.read_text().split('\n---\n') if s.strip()]
for obj in objects(root/'deploy/kubernetes/baseline.yaml'):
 if obj['apiVersion'].startswith('networking.istio'): continue
 if obj['kind']=='Namespace':
  obj['metadata']['labels'].pop('istio-injection',None)
  if provider=='linkerd': obj['metadata']['annotations']={'linkerd.io/inject':'enabled'}
  if provider=='istio': obj['metadata']['labels']['istio.io/rev']='default'
 if obj['kind']=='Deployment' and provider=='linkerd': obj['spec']['template']['metadata']['annotations']={'linkerd.io/inject':'enabled'}
 apply(obj)
controller='io.cilium/gateway-controller' if provider=='cilium' else 'gateway.envoyproxy.io/gatewayclass-controller'
cls='cilium' if provider=='cilium' else 'eg'
if provider=='linkerd':
 apply({'apiVersion':'gateway.envoyproxy.io/v1alpha1','kind':'EnvoyProxy','metadata':{'name':'envy-test','namespace':'envoy-gateway-system'},'spec':{'provider':{'type':'Kubernetes','kubernetes':{'envoyService':{'type':'NodePort','patch':{'type':'StrategicMerge','value':{'spec':{'ports':[{'port':port,'nodePort':30080}]}}}}}}}})
if provider!='istio': apply({'apiVersion':'gateway.networking.k8s.io/v1','kind':'GatewayClass','metadata':{'name':cls},'spec':dict(controllerName=controller,**({'parametersRef':{'group':'gateway.envoyproxy.io','kind':'EnvoyProxy','name':'envy-test','namespace':'envoy-gateway-system'}} if provider=='linkerd' else {}))})
listener={'name':'http','port':port,'protocol':'HTTP','hostname':'*.envy.localhost'}
if https:
 subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','2','-subj','/CN=*.envy.localhost','-addext','subjectAltName=DNS:*.envy.localhost','-keyout',str(state/'preview.key'),'-out',str(state/'preview.crt')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 apply({'apiVersion':'v1','kind':'Secret','metadata':{'name':'preview-tls','namespace':'istio-system' if provider=='istio' else 'envy-baseline'},'type':'kubernetes.io/tls','stringData':{'tls.crt':(state/'preview.crt').read_text(),'tls.key':(state/'preview.key').read_text()}})
 listener.update(name='https',protocol='HTTPS',tls={'mode':'Terminate','certificateRefs':[{'name':'preview-tls'}]})
if provider=='istio':
 server={'port':{'number':port,'name':'preview','protocol':'HTTPS' if https else 'HTTP'},'hosts':['*.envy.localhost']}
 if https:server['tls']={'mode':'SIMPLE','credentialName':'preview-tls'}
 apply({'apiVersion':'networking.istio.io/v1','kind':'Gateway','metadata':{'name':'envy-preview','namespace':'envy-baseline'},'spec':{'selector':{'istio':'ingressgateway'},'servers':[server]}})
 apply({'apiVersion':'networking.istio.io/v1','kind':'VirtualService','metadata':{'name':'baseline','namespace':'envy-baseline'},'spec':{'hosts':['baseline.envy.localhost'],'gateways':['envy-preview'],'http':[{'headers':{'request':{'remove':['baggage']}},'route':[{'destination':{'host':'gateway.envy-baseline.svc.cluster.local','port':{'number':8080}}}]}]}})
else:
 apply({'apiVersion':'gateway.networking.k8s.io/v1','kind':'Gateway','metadata':{'name':'envy-preview','namespace':'envy-baseline'},'spec':{'gatewayClassName':cls,'listeners':[listener]}})
 apply({'apiVersion':'gateway.networking.k8s.io/v1','kind':'HTTPRoute','metadata':{'name':'baseline','namespace':'envy-baseline'},'spec':{'parentRefs':[{'name':'envy-preview'}],'hostnames':['baseline.envy.localhost'],'rules':[{'filters':[{'type':'RequestHeaderModifier','requestHeaderModifier':{'remove':['baggage']}}],'backendRefs':[{'name':'gateway','port':8080}]}]}})
serviceNS={'istio':'istio-system','cilium':'envy-baseline','linkerd':'envoy-gateway-system'}[provider]
for attempt in range(90):
 svcs=json.loads(run('kubectl','get','services','-n',serviceNS,'-o','json'))
 found=[s for s in svcs['items'] if (provider=='istio' and s['metadata']['name']=='istio-ingressgateway') or (provider=='cilium' and s['metadata']['name']=='cilium-gateway-envy-preview') or s['metadata'].get('labels',{}).get('gateway.envoyproxy.io/owning-gateway-name')=='envy-preview']
 if found:break
 time.sleep(2)
else:raise SystemExit('Gateway controller did not create a data-plane Service')
svc=found[0]; name=svc['metadata']['name']
serviceType='LoadBalancer' if provider=='cilium' else 'NodePort'
if provider=='istio':
 ports=[p for p in svc['spec']['ports'] if p['port'] not in [80,port]]
 ports.append({'name':'envy-preview','port':port,'targetPort':port,'nodePort':30080})
else:
 p=svc['spec']['ports'][0];ports=[dict(p,nodePort=30080)]
if provider!='linkerd':run('kubectl','patch','svc',name,'-n',serviceNS,'--type=merge','-p',json.dumps({'spec':{'type':serviceType,'ports':ports}}))
ingress=f'{scheme}://{name}.{serviceNS}.svc.cluster.local:{port}'
for svc in ['gateway','service-a','service-b']: run('kubectl','-n','envy-baseline','rollout','status','deployment/'+svc,'--timeout=180s')
if provider!='istio':run('kubectl','wait','gateway/envy-preview','-n','envy-baseline','--for=condition=Programmed','--timeout=180s')
probe=['curl','--silent','--fail','--max-time','3','--output','/dev/null','--resolve',f'baseline.envy.localhost:{port}:127.0.0.1',f'{scheme}://baseline.envy.localhost:{port}/']
if https:probe.extend(['--cacert',str(state/'preview.crt')])
for attempt in range(60):
 if subprocess.run(probe,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
 time.sleep(2)
else:raise SystemExit('Baseline ingress did not become reachable before installing Envy')

apply({'apiVersion':'v1','kind':'Namespace','metadata':{'name':'envy-system'}})
existing=run('kubectl','get','secret','envy-test','-n','envy-system','--ignore-not-found','-o','json')
if existing:
 data=json.loads(existing)['data'];token=base64.b64decode(data['token']).decode();password=base64.b64decode(data['password']).decode()
else:token=secrets.token_hex(24);password=secrets.token_hex(24)
(state/'api-token').write_text(token);(state/'api-token').chmod(0o600)
apply({'apiVersion':'v1','kind':'Secret','metadata':{'name':'envy-test','namespace':'envy-system'},'stringData':{'token':token,'password':password,'url':f'postgres://envy:{password}@postgres.envy-system.svc.cluster.local:5432/envy?sslmode=disable'}})
apply({'apiVersion':'v1','kind':'PersistentVolumeClaim','metadata':{'name':'postgres-data','namespace':'envy-system'},'spec':{'accessModes':['ReadWriteOnce'],'resources':{'requests':{'storage':'1Gi'}}}})
apply({'apiVersion':'apps/v1','kind':'Deployment','metadata':{'name':'postgres','namespace':'envy-system'},'spec':{'selector':{'matchLabels':{'app':'postgres'}},'template':{'metadata':{'labels':{'app':'postgres'}},'spec':{'volumes':[{'name':'data','persistentVolumeClaim':{'claimName':'postgres-data'}}],'containers':[{'name':'postgres','volumeMounts':[{'name':'data','mountPath':'/var/lib/postgresql'}],'image':'postgres:18.6@sha256:4ef4dbc939d61acea57712655ddb4b4ab27419c913f94cca0cd57cb3ea3c2280','readinessProbe':{'exec':{'command':['pg_isready','-h','127.0.0.1','-U','envy','-d','envy']},'periodSeconds':2},'env':[{'name':'POSTGRES_DB','value':'envy'},{'name':'POSTGRES_USER','value':'envy'},{'name':'POSTGRES_PASSWORD','valueFrom':{'secretKeyRef':{'name':'envy-test','key':'password'}}}]}]}}}})
apply({'apiVersion':'v1','kind':'Service','metadata':{'name':'postgres','namespace':'envy-system'},'spec':{'selector':{'app':'postgres'},'ports':[{'port':5432}]}})
run('kubectl','-n','envy-system','rollout','status','deployment/postgres','--timeout=180s')
values={'installationID':'envy-local','mesh':{'provider':provider},'image':{'repository':'envy/server','tag':'mesh-test','pullPolicy':'IfNotPresent'},'externalDatabase':{'secretName':'envy-test','secretKey':'url'},'auth':{'mode':'token','proxySecret':{'name':''},'sharedTokenSecret':{'name':'envy-test','key':'token'},'externalOrigin':f'http://localhost:{api}'},'runtime':{'previewBaseURL':f'{scheme}://envy.localhost:{port}','baselineHost':'baseline.envy.localhost','ingressURL':ingress},'networkPolicy':{'enabled':False},'preflight':{'enabled':True,'curlImage':'curlimages/curl:8.14.1@'+versions['images']['curlimages/curl:8.14.1']},'service':{'type':'NodePort','nodePort':30081},'gatewayAPI':{'gatewayClass':cls}}
if https:
 apply({'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':'preview-ca','namespace':'envy-system'},'data':{'ca.crt':(state/'preview.crt').read_text()}})
 values['runtime']['caConfigMap']={'name':'preview-ca','key':'ca.crt'}
(state/'values.json').write_text(json.dumps(values))
chart=root/'deploy/helm/envy';initial_values=state/'values.json'
upgrade=provider=='istio' and os.environ.get('ENVY_TEST_ISTIO_UPGRADE','1')=='1'
if upgrade:
 subprocess.run(['bash',str(root/'deploy/testing/prepare-istio-upgrade.sh')],check=True)
 chart=state/'upgrade-source/deploy/helm/envy'
 initial=json.loads(json.dumps(values));initial['image']['tag']='upgrade-from'
 initial_values=state/'upgrade-values.json';initial_values.write_text(json.dumps(initial))
subprocess.run(['helm','upgrade','--install','envy',str(chart),'-n','envy-system','-f',str(initial_values),'--wait','--wait-for-jobs','--timeout','5m'],check=True)
components=[];bindings={}
for name,down in [('gateway','service-a'),('service-a','service-b'),('service-b',None)]:
 c={'id':name,'project':'demo','protocol':'http','port':8080,'profile':'http-small','health_path':'/healthz','readiness_path':'/readyz','overridable':True}
 if down:c['env']={'DOWNSTREAM_URL':f'http://{down}.envy-baseline.svc.cluster.local:8080'}
 components.append(c);bindings[name]={'service_host':f'{name}.envy-baseline.svc.cluster.local','port':8080,'image':f'envy/{name}:v1'}
manifest={'api_version':'envy/v1','project':{'id':'demo','name':'Demo'},'components':components,'baseline':{'id':'staging','project':'demo','revision':'demo-v1','endpoint':f'{scheme}://baseline.envy.localhost:{port}','routing':{'namespace':'envy-baseline','gateway':'envy-preview','entry_component':'gateway'},'verification':{'kind':'envy-chain','chain':['gateway','service-a','service-b']},'components':bindings}}
(state/'catalog.json').write_text(json.dumps(manifest))
os.environ.update(ENVY_API_URL=f'http://127.0.0.1:{api}',ENVY_API_TOKEN_FILE=str(state/'api-token'))
if provider!='istio': run('kubectl','wait','gateway/envy-preview','-n','envy-baseline','--for=condition=Programmed','--timeout=180s')
subprocess.run([str(root/'.envy/bin/delivery'),'catalog','apply','--file',str(state/'catalog.json')],check=True)
if upgrade:subprocess.run(['python3',str(root/'deploy/testing/verify-istio-upgrade.py')],check=True)
(state/'ingress-url').write_text(ingress)

preflight={'installation_id':'envy-local','mesh':{'provider':provider},'namespace':'envy-system','gateway':{'namespace':'envy-baseline','name':'envy-preview'},'gateway_class':cls,'database_secret':{'name':'envy-test','key':'url'},'preview_base_url':f'{scheme}://envy.localhost:{port}','baseline_host':'baseline.envy.localhost','ingress_url':ingress,'catalog':manifest}
if provider=='istio':preflight.update(injection_labels={'istio.io/rev':'default'},ingress_selector={'istio':'ingressgateway'})
(state/'installation.json').write_text(json.dumps(preflight))
check=subprocess.run([str(root/'.envy/bin/delivery'),'installation','check','--file',str(state/'installation.json')],capture_output=True,text=True)
(state/'preflight.json').write_text(check.stdout)
if check.returncode not in [0,2]:raise SystemExit('Installation preflight failed: '+check.stdout+check.stderr)
