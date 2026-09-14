// Shared by Astro configuration, components, and Markdown transforms.
export const siteOrigin = "https://dblooman.github.io";
export const siteBase = "/envy/";

export function withBase(url) {
  if (!url.startsWith("/") || url.startsWith("//")) return url;
  if (url === siteBase.slice(0, -1) || url.startsWith(siteBase)) return url;
  return siteBase + url.slice(1);
}

// Markdown links remain readable and independent of the hosting prefix.
export function remarkBaseLinks() {
  return function transform(tree) {
    function walk(node) {
      if (["link", "image", "definition"].includes(node.type)) {
        node.url = withBase(node.url);
      }
      for (const child of node.children ?? []) walk(child);
    }
    walk(tree);
  };
}
