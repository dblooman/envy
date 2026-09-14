import ELK from "elkjs/lib/elk.bundled.js";
import type { ElkNode } from "elkjs/lib/elk-api";
import { diagrams } from "./graphs.ts";

const elk = new ELK();
const cache = new Map<string, Promise<ElkNode>>();

export function layoutDiagram(name: string, direction: "DOWN" | "RIGHT") {
  const key = `${name}-${direction}`;
  if (!cache.has(key)) {
    const diagram = diagrams[name];
    if (!diagram) throw new Error(`Unknown diagram: ${name}`);
    const ids = new Set(diagram.nodes.map((node) => node.id));
    if (ids.size !== diagram.nodes.length)
      throw new Error(`Duplicate nodes in ${name}`);
    for (const edge of diagram.edges) {
      if (!ids.has(edge.from) || !ids.has(edge.to))
        throw new Error(`Unconnected edge in ${name}`);
    }
    cache.set(
      key,
      elk.layout({
        id: key,
        layoutOptions: {
          "elk.algorithm": "layered",
          "elk.direction": direction,
          "elk.edgeRouting": "ORTHOGONAL",
          "elk.padding": "[top=24,left=24,bottom=24,right=24]",
          "elk.spacing.nodeNode": "32",
          "elk.layered.spacing.nodeNodeBetweenLayers": "24",
          "elk.layered.spacing.edgeNodeBetweenLayers": "8",
          "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
        },
        children: diagram.nodes.map((node) => ({
          id: node.id,
          width: 212,
          height: 82,
        })),
        edges: diagram.edges.map((edge, index) => ({
          id: `edge-${index}`,
          sources: [edge.from],
          targets: [edge.to],
          labels: [
            {
              text: edge.label,
              width: edge.label.length * 6.5 + 16,
              height: 24,
              layoutOptions: { "elk.edgeLabels.inline": "false" },
            },
          ],
        })),
      }),
    );
  }
  return cache.get(key)!;
}
