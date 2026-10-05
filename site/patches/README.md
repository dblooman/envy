# Astro parser glob dependency

The patch for `astro-eslint-parser@1.4.0` switches project discovery from
`fast-glob` to `tinyglobby`, following the migration in
[astro-eslint-parser 2.1.0](https://github.com/ota-meshi/astro-eslint-parser/releases/tag/v2.1.0).
This removes the transitive `braces` dependency affected by
[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm), which has no
patched release. Both the CommonJS and ESM builds are patched.

The version-scoped pnpm override removes `fast-glob`, and the package extension
adds `tinyglobby`. Keep these settings with the patch. Remove all three when
upgrading the Astro lint tooling to a version that uses `tinyglobby` upstream.
The newer lint tooling requires Node 24.16+ and ESLint 10; this backport keeps
the current Node 24.8 and ESLint 9 baseline working.
