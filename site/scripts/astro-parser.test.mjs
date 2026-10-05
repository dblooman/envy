import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import astro from "eslint-plugin-astro";
import tseslint from "typescript-eslint";

const parser = astro.configs.base.find(
  (config) =>
    config.languageOptions?.parser?.meta?.name === "astro-eslint-parser",
).languageOptions.parser;

test("Astro parser resolves project globs and exclusions", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "envy-astro-parser-"));
  try {
    const source = "---\nconst value: number = 1;\n---\n<p>{value}</p>";
    const filePath = path.join(root, "example.astro");
    const configPath = path.join(root, "primary/tsconfig.json");
    await writeFile(filePath, source);
    await mkdir(path.join(root, "primary"));
    await mkdir(path.join(root, "ignored"));
    await writeFile(
      configPath,
      JSON.stringify({
        compilerOptions: { target: "ESNext", module: "ESNext" },
        include: ["../*.astro"],
      }),
    );
    await writeFile(path.join(root, "ignored/tsconfig.json"), "invalid JSON");

    const result = parser.parseForESLint(source, {
      parser: tseslint.parser,
      filePath,
      project: ["{primary,ignored}/tsconfig.json"],
      projectFolderIgnoreList: ["ignored/**"],
      tsconfigRootDir: root,
    });

    assert.equal(
      await realpath(
        result.services.program.getCompilerOptions().configFilePath,
      ),
      await realpath(configPath),
    );
    assert.deepEqual(
      await Promise.all(
        result.services.program
          .getRootFileNames()
          .map((file) => realpath(file)),
      ),
      [await realpath(filePath)],
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
