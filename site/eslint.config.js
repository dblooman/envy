import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import tseslint from "typescript-eslint";
import astro from "eslint-plugin-astro";
import globals from "globals";

export default defineConfig(
  { ignores: ["dist/**", ".astro/**", "node_modules/**"] },
  js.configs.recommended,
  tseslint.configs.recommended,
  ...astro.configs.recommended,
  ...astro.configs["jsx-a11y-recommended"],
  { languageOptions: { globals: { ...globals.node, ...globals.browser } } },
  {
    files: ["**/*.astro"],
    rules: {
      "astro/jsx-a11y/no-noninteractive-tabindex": [
        "error",
        { roles: ["region"] },
      ],
    },
  },
);
