#!/usr/bin/env node
// No shell interpolation: the application's build executable and args stay separate.
import { readFile } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { pathToFileURL } from 'node:url';

export function configuration(config, env) {
  const revision = env.CF_PAGES_COMMIT_SHA || env.ENVY_FRONTEND_REVISION;
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(revision || '')) throw new Error('A full lowercase frontend commit SHA is required');
  if (env.CF_PAGES_COMMIT_SHA && env.ENVY_FRONTEND_REVISION && env.CF_PAGES_COMMIT_SHA !== env.ENVY_FRONTEND_REVISION) throw new Error('Frontend revision variables disagree');
  for (const key of ['project', 'frontend']) if (!/^(?:[a-z][a-z0-9-]{0,61}[a-z0-9]|[a-z])$/.test(config[key] || '')) throw new Error(`Invalid ${key}`);
  if (!/^(VITE_|NEXT_PUBLIC_|PUBLIC_)[A-Z0-9_]+$/.test(config.api_url_env || '') || /TOKEN|SECRET|PASSWORD|CREDENTIAL/.test(config.api_url_env)) throw new Error('api_url_env must be a public URL build variable');
  const path = config.api_path || '/';
  if (!/^\/(?!\/)[a-zA-Z0-9/_-]*$/.test(path)) throw new Error('api_path must be an absolute URL path without query or fragment');
  if (!env.CF_PAGES_URL) throw new Error('CF_PAGES_URL is required (use a loopback frontend URL for local builds)');
  safeURL(env.CF_PAGES_URL, env.CF_PAGES === '1');
  if (env.CF_PAGES === '1') safeURL(env.ENVY_API_URL, true);
  return { ...config, revision, api_path: path };
}
function safeURL(raw, httpsOnly) {
  const url = new URL(raw);
  const loopback = url.hostname === 'localhost' || url.hostname.endsWith('.localhost') || url.hostname === '127.0.0.1' || url.hostname === '[::1]';
  if (url.username || url.password || url.search || url.hash || (url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback && !httpsOnly))) throw new Error('Cloud builds require HTTPS URLs; HTTP is allowed only for local loopback builds');
  return url;
}
export function buildEnvironment(env, config, apiURL) {
  const child = { ...env };
  for (const key of Object.keys(child)) if (key.startsWith('ENVY_')) delete child[key];
  child[config.api_url_env] = apiURL;
  return child;
}
function run(executable, args, env, capture = false) {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, { env, stdio: ['ignore', capture ? 'pipe' : 'inherit', 'inherit'] });
    let output = '';
    child.stdout?.on('data', chunk => { output += chunk; if (output.length > 1024 * 1024) child.kill(); });
    child.on('error', reject);
    child.on('close', code => code === 0 ? resolve(output) : reject(new Error(`Command failed with exit status ${code}; build/publication stopped`)));
  });
}
export async function build(args, env = process.env, execute = run) {
  const split = args.indexOf('--');
  if (split !== 2 || args[0] !== '--config' || !args[3]) throw new Error('Usage: node build.mjs --config frontend.envy.json -- executable [args]');
  const config = configuration(JSON.parse(await readFile(args[1], 'utf8')), env);
  const key = ['--project', config.project, '--frontend', config.frontend, '--revision', config.revision];
  const cli = env.ENVY_DELIVERY_BINARY || 'envy';
  const receipt = JSON.parse(await execute(cli, ['frontend', 'resolve', ...key, '--timeout', '60s'], env, true));
  if (receipt.project !== config.project || receipt.frontend !== config.frontend || receipt.revision !== config.revision || !receipt.composition || !Number.isSafeInteger(receipt.binding_version) || receipt.binding_version < 1 || Date.parse(receipt.expires_at) <= Date.now() || !Number.isFinite(Date.parse(receipt.expires_at))) throw new Error('Resolver returned an invalid or expired binding receipt');
  const api = safeURL(receipt.api_url, env.CF_PAGES === '1');
  api.pathname = config.api_path;
  await execute(args[3], args.slice(4), buildEnvironment(env, config, api.href));
  await execute(cli, ['frontend', 'publish', ...key, '--expected-version', String(receipt.binding_version), '--url', env.CF_PAGES_URL], env, true);
  process.stderr.write('Build completed; frontend URL recorded. Verify the hosted browser path separately.\n');
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  build(process.argv.slice(2)).catch(error => { process.stderr.write(`${error.message}\n`); process.exitCode = 1; });
}
