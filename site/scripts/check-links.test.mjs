import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const checker = fileURLToPath(new URL("./check-links.mjs", import.meta.url));
test("crawl handles project root, anchors, assets, and broken paths", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "envy-site-links-"));
  try {
    await mkdir(path.join(root, "pagefind"));
    await writeFile(path.join(root, "pagefind/pagefind.js"), "");
    await writeFile(path.join(root, "style.css"), "");
    await writeFile(
      path.join(root, "index.html"),
      '<h1 id="top">Envy</h1><a href="/envy/#top">Home</a><link href="/envy/style.css"><a href="https://example.com/">External</a>',
    );
    let result = spawnSync(process.execPath, [checker, root], {
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr);
    await writeFile(
      path.join(root, "index.html"),
      '<a href="/envy/missing/">Missing</a><a href="/wrong/">Escapes base</a><a href="#absent">Bad anchor</a>',
    );
    result = spawnSync(process.execPath, [checker, root], { encoding: "utf8" });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /missing file/);
    assert.match(result.stderr, /URL escapes/);
    assert.match(result.stderr, /missing anchor/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
