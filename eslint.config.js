import astro from "eslint-plugin-astro";
import tseslint from "typescript-eslint";

export default [
  {
    ignores: [
      ".astro/**",
      ".worktrees/**",
      ".pi/**",
      "bin/**",
      "coverage/**",
      "dist/**",
      "node_modules/**",
      "static/**",
      "storybook-server/**",
      "storybook-static/**",
    ],
  },
  ...tseslint.configs.recommended,
  ...astro.configs.recommended,
  {
    rules: {
      "@typescript-eslint/ban-ts-comment": "off",
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-unused-expressions": "off",
      "@typescript-eslint/no-unused-vars": "off",
      "no-useless-assignment": "off",
      "no-control-regex": "off",
      "no-empty": "off",
      "no-unused-expressions": "off",
    },
  },
];
