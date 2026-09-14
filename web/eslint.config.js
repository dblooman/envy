import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import tseslint from "typescript-eslint";
import globals from "globals";
import hooks from "eslint-plugin-react-hooks";
import jsxA11y from "eslint-plugin-jsx-a11y";

export default defineConfig(
  { ignores: ["dist/**", "node_modules/**"] },
  js.configs.recommended,
  tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { "react-hooks": hooks, "jsx-a11y": jsxA11y },
    settings: { "jsx-a11y": { components: { Input: "input" } } },
    rules: {
      ...hooks.configs.recommended.rules,
      ...jsxA11y.configs.recommended.rules,
      "jsx-a11y/label-has-associated-control": [
        "error",
        { controlComponents: ["Input"], depth: 3 },
      ],
    },
  },
);
