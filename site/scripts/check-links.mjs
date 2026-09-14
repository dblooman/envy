import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "parse5";
import { siteBase, siteOrigin } from "../src/lib/paths.mjs";

const root = path.resolve(
  process.argv[2] || fileURLToPath(new URL("../dist/", import.meta.url)),
);
const documents = new Map();
const errors = [];
async function files(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  return (
    await Promise.all(
      entries.map((e) =>
        e.isDirectory()
          ? files(path.join(dir, e.name))
          : [path.join(dir, e.name)],
      ),
    )
  ).flat();
}
function walk(node, callback) {
  callback(node);
  for (const child of node.childNodes ?? []) walk(child, callback);
}
const builtFiles = await files(root);
for (const file of builtFiles.filter((f) => f.endsWith(".html"))) {
  const dom = parse(await readFile(file, "utf8"));
  const ids = new Set();
  const links = [];
  walk(dom, (node) => {
    for (const attr of node.attrs ?? []) {
      if (attr.name === "id") ids.add(attr.value);
      if (["href", "src", "poster"].includes(attr.name)) links.push(attr.value);
      if (attr.name === "srcset" && !attr.value.startsWith("data:")) {
        links.push(
          ...attr.value.split(",").map((entry) => entry.trim().split(/\s+/)[0]),
        );
      }
    }
  });
  documents.set(file, { ids, links });
}
async function check(link, source) {
  const relative = path
    .relative(root, source)
    .split(path.sep)
    .join("/")
    .replace(/index\.html$/, "");
  const url = new URL(link, siteOrigin + siteBase + relative);
  if (url.origin !== siteOrigin) return;
  if (!url.pathname.startsWith(siteBase)) {
    errors.push(`${relative}: URL escapes ${siteBase}: ${link}`);
    return;
  }
  let target = path.resolve(
    root,
    decodeURIComponent(url.pathname.slice(siteBase.length)),
  );
  if (target !== root && !target.startsWith(root + path.sep)) {
    errors.push(`${relative}: URL escapes build directory: ${link}`);
    return;
  }
  try {
    if ((await stat(target)).isDirectory())
      target = path.join(target, "index.html");
    await stat(target);
    const doc = documents.get(target);
    if (
      doc &&
      url.hash &&
      !doc.ids.has(decodeURIComponent(url.hash.slice(1)))
    ) {
      errors.push(`${relative}: missing anchor: ${link}`);
    }
  } catch {
    errors.push(`${relative}: missing file: ${link}`);
  }
}
for (const [source, { links }] of documents) {
  for (const link of links) await check(link, source);
}
for (const source of builtFiles.filter((f) => f.endsWith(".css"))) {
  const css = await readFile(source, "utf8");
  for (const match of css.matchAll(/url\(\s*['"]?([^'"\s)]+)['"]?\s*\)/g))
    await check(match[1], source);
}
for (const source of builtFiles.filter((f) => /sitemap.*\.xml$/.test(f))) {
  for (const match of (await readFile(source, "utf8")).matchAll(
    /<loc>([^<]+)<\/loc>/g,
  )) {
    if (!match[1].startsWith(siteOrigin + siteBase))
      errors.push(`Wrong sitemap origin/base: ${match[1]}`);
    await check(match[1], source);
  }
}
await stat(path.join(root, "pagefind/pagefind.js"));
if (errors.length) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(
    `Validated links, anchors, assets, sitemap, and search output for ${documents.size} pages under ${siteBase}.`,
  );
}
