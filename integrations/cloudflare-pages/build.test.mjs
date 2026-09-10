import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { build, configuration } from './build.mjs';
const config = {project:'shop', frontend:'web', api_url_env:'VITE_ENVY_API_URL', api_path:'/products'};
const env = {CF_PAGES_COMMIT_SHA:'a'.repeat(40), CF_PAGES_URL:'https://web.pages.dev', CF_PAGES:'1', ENVY_API_TOKEN:'secret', ENVY_API_TOKEN_FILE:'/private/token', ENVY_API_URL:'https://envy.example', PATH:process.env.PATH};
const receipt = {project:'shop',frontend:'web',revision:env.CF_PAGES_COMMIT_SHA,composition:'abc',binding_version:2,api_url:'https://preview.example',expires_at:new Date(Date.now()+60000).toISOString()};
test('configuration rejects branch guesses, credentials and mixed content',()=>{
 for(const altered of [{...env,ENVY_API_URL:'http://control.example'},{...env,CF_PAGES_COMMIT_SHA:'main'},{...env,ENVY_FRONTEND_REVISION:'b'.repeat(40)},{...env,CF_PAGES_URL:'http://localhost:4174'},{...env,CF_PAGES_URL:'https://user:pass@pages.dev'}]) assert.throws(()=>configuration(config,altered));
 for(const altered of [{...config,api_url_env:'ENVY_API_TOKEN'},{...config,api_path:'//evil.example'},{...config,api_path:'/../x'}]) assert.throws(()=>configuration(altered,env));
});
for(const failure of ['none','resolve','build','expired','http']) test(`build boundaries: ${failure}`,async()=>{
 const dir=await mkdtemp(join(tmpdir(),'envy-adapter-'));const file=join(dir,'config.json'); await writeFile(file,JSON.stringify(config));
 const calls=[];
 try {
  const invoke=async(exe,args,child)=>{
   calls.push(args);
   if(args[1]==='resolve') { if(failure==='resolve')throw new Error('gone'); return JSON.stringify({...receipt,...(failure==='expired'?{expires_at:'2000-01-01'}:{}),...(failure==='http'?{api_url:'http://localhost:8080'}:{})}); }
   if(exe==='build-app') {
    assert.equal(child.VITE_ENVY_API_URL,'https://preview.example/products');
    for(const key of ['ENVY_API_TOKEN','ENVY_API_TOKEN_FILE','ENVY_API_URL']) assert.equal(child[key],undefined);
    assert.deepEqual(args,['literal $(do-not-execute)']); if(failure==='build')throw new Error('build failed');
   } else if(args[1]==='publish') {assert.equal(child.ENVY_API_TOKEN,'secret');assert.equal(args[args.indexOf('--expected-version')+1],'2');}
  };
  const promise=build(['--config',file,'--','build-app','literal $(do-not-execute)'],env,invoke);
  if(failure==='none') {await promise;assert.equal(calls.length,3);} else {await assert.rejects(promise);assert.equal(calls.some(args=>args[1]==='publish'),false);}
 } finally {await rm(dir,{recursive:true,force:true});}
});
