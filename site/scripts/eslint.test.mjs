import test from "node:test";
import assert from "node:assert/strict";
import { ESLint } from "eslint";

const eslint = new ESLint();
const filePath = "src/components/AccessibilityFixture.astro";

test("Astro accessibility lint catches missing text and invalid ARIA values", async () => {
  const [result] = await eslint.lintText(
    '<img src="/diagram.svg" />\n<a href="/docs/"></a>\n<input aria-invalid="bogus" />\n<div tabindex="0">Content</div>',
    { filePath },
  );
  assert.equal(result.fatalErrorCount, 0);
  const rules = new Set(result.messages.map((message) => message.ruleId));
  for (const rule of [
    "alt-text",
    "anchor-has-content",
    "aria-proptypes",
    "no-noninteractive-tabindex",
  ]) {
    assert.ok(rules.has(`astro/jsx-a11y/${rule}`), rule);
  }
});

test("Astro accessibility lint accepts accessible content and focusable regions", async () => {
  const [result] = await eslint.lintText(
    '<img src="/diagram.svg" alt="Architecture diagram" />\n<a href="/docs/">Docs</a>\n<input aria-invalid="false" aria-label="Search" />\n<div role="region" aria-label="Example" tabindex="0">Content</div>',
    { filePath },
  );
  assert.equal(result.errorCount, 0, JSON.stringify(result.messages));
  assert.equal(result.warningCount, 0, JSON.stringify(result.messages));
});
