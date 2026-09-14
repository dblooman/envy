import test from "node:test";
import assert from "node:assert/strict";
import { withBase, remarkBaseLinks } from "./paths.mjs";

test("prefix internal paths once while preserving anchors and queries", () => {
  assert.equal(withBase("/"), "/envy/");
  assert.equal(
    withBase("/reference/api/?q=route#auth"),
    "/envy/reference/api/?q=route#auth",
  );
  assert.equal(withBase("/envy/reference/api/"), "/envy/reference/api/");
  for (const url of [
    "#auth",
    "../api/",
    "https://example.com/",
    "//example.com/image.svg",
    "mailto:test@example.com",
  ]) {
    assert.equal(withBase(url), url);
  }
});
test("Markdown links, images, and reference definitions share the same prefix", () => {
  const tree = {
    type: "root",
    children: ["link", "image", "definition"].map((type) => ({
      type,
      url: "/overview/",
    })),
  };
  remarkBaseLinks()(tree);
  assert.ok(tree.children.every((node) => node.url === "/envy/overview/"));
  remarkBaseLinks()(tree);
  assert.ok(tree.children.every((node) => node.url === "/envy/overview/"));
});
