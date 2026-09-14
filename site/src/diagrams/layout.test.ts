import test from "node:test";
import assert from "node:assert/strict";
import { diagrams } from "./graphs.ts";
import { layoutDiagram } from "./layout.ts";

for (const [name, definition] of Object.entries(diagrams)) {
  test(`${name}: every connection is routed without crossing a service card`, async () => {
    const graph = await layoutDiagram(name, "DOWN");
    const nodes = graph.children ?? [];
    assert.equal(nodes.length, definition.nodes.length);
    assert.equal(graph.edges?.length, definition.edges.length);
    for (const edge of graph.edges ?? []) {
      assert.ok(edge.sections?.length, `${edge.id} has no connector`);
      for (const section of edge.sections ?? []) {
        const points = [
          section.startPoint,
          ...(section.bendPoints ?? []),
          section.endPoint,
        ];
        for (let index = 1; index < points.length; index++) {
          const a = points[index - 1],
            b = points[index];
          assert.ok(
            a.x === b.x || a.y === b.y,
            "Connectors must be orthogonal",
          );
          for (const node of nodes) {
            const x = node.x!,
              y = node.y!,
              right = x + node.width!,
              bottom = y + node.height!;
            const verticalCross =
              a.x === b.x &&
              a.x > x &&
              a.x < right &&
              Math.max(a.y, b.y) > y &&
              Math.min(a.y, b.y) < bottom;
            const horizontalCross =
              a.y === b.y &&
              a.y > y &&
              a.y < bottom &&
              Math.max(a.x, b.x) > x &&
              Math.min(a.x, b.x) < right;
            assert.ok(
              !verticalCross && !horizontalCross,
              `${edge.id} crosses ${node.id}`,
            );
          }
        }
      }
    }
    for (const node of nodes) {
      assert.ok(node.x! >= 0 && node.y! >= 0);
      assert.ok(
        node.x! + node.width! <= graph.width! &&
          node.y! + node.height! <= graph.height!,
      );
    }
  });
}
