import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';

export default [
  { ignores: ['dist/'] },
  js.configs.recommended,
  ...svelte.configs.recommended,
  { languageOptions: { globals: globals.browser } },
  { files: ['src/sw.js'], languageOptions: { globals: globals.serviceworker } },
  { files: ['test/**', '*.config.js'], languageOptions: { globals: globals.node } },
  {
    rules: {
      'no-empty': ['error', { allowEmptyCatch: true }],
      'no-unused-vars': ['error', { varsIgnorePattern: '^_', argsIgnorePattern: '^_', ignoreRestSiblings: true }],
      'svelte/require-each-key': 'off',
      'svelte/no-useless-mustaches': 'off',
      'svelte/no-reactive-functions': 'off',
      'svelte/infinite-reactive-loop': 'off',
    },
  },
  {
    files: ['**/*.svelte'],
    rules: { 'no-useless-assignment': 'off' },
  },
];
