import { mkdir, writeFile, copyFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const apiURL = process.env.VITE_ENVY_API_URL;
if (!apiURL) throw new Error('VITE_ENVY_API_URL is required; no staging fallback');
const out = new URL('./dist/', import.meta.url);
await mkdir(out, {recursive:true});
await copyFile(new URL('./index.html',import.meta.url), new URL('index.html',out));
await writeFile(new URL('config.js',out), `window.SHOP_API_URL = ${JSON.stringify(apiURL)};\n`);
console.log(`Built shop frontend in ${fileURLToPath(out)}`);
