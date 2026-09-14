# Third-party license audit

Reviewed 2026-09-14 for source publication. Envy's original code remains MIT;
third-party code retains its own license. This inventory describes the dependencies
reviewed, not a relicensing of them or a complete binary-distribution notice bundle.

## Go

`go-licenses report ./cmd/...` identified 87 package license entries:
48 Apache-2.0, 20 BSD-3-Clause, 18 MIT (including Envy), and one ISC.
The scanner warned that assembly cannot be recursively inspected; those files
remain covered by their containing upstream packages' license notices. It also
reported the local Envy module as HEAD, whose license was verified directly.

## JavaScript and site tooling

`pnpm licenses list --json` was run in both `web/` and `site/`. All scanned entries
have declared licenses. Most are MIT, ISC, Apache-2.0, or BSD. The less common
licenses and their roles deserve explicit tracking:

| Package | Declared license | Role |
| --- | --- | --- |
| `elkjs` | EPL-2.0 OR GPL-3.0-or-later | Runs at site build time to generate SVG diagrams; no ELK JavaScript is sent to the browser. |
| `@img/sharp-libvips-*` | LGPL-3.0-or-later | Native site image build dependency; not included in the static Pages output or server image. |
| `lightningcss` and native packages | MPL-2.0 | CSS build tooling. |
| `axe-core` | MPL-2.0 | Accessibility lint dependency. |
| `caniuse-lite` | CC-BY-4.0 | Browser compatibility data used by build tooling. |
| `argparse` | Python-2.0 | JavaScript tooling parser. |
| `@base-ui-components/react` | MIT | Dashboard controls; upstream renamed the package to `@base-ui/react`. Migration is a separate compatibility change. |

Browser bundle inspection confirms React and other upstream license comments are
retained. The unused Astro starter image was removed. Envy's SVG logo/favicon
remain in the repository; their ownership should be confirmed by the maintainer
as part of publication, since automated license scanners cannot establish authorship.

## Reproduce and maintain

```sh
go install github.com/google/go-licenses@v1.6.0
go-licenses report ./cmd/...
(cd web && pnpm licenses list --json)
(cd site && pnpm licenses list --json)
```

Review licenses again when dependencies change. Preserve upstream license and
NOTICE files when packaging release binaries/images or copying dependencies. A
future binary release should generate and ship a notice bundle for that release's
actual contents, including platform-specific/native dependencies. Source publication
and static-site deployment do not publish `node_modules` or native build tools.
