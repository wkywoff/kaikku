import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';

// Линтер ловит то, чего не видит svelte-check (checkJs выключен): опечатки в
// именах, забытые импорты, неиспользуемый код. Запуск: npm run lint.
export default [
  { ignores: ['dist/'] },
  js.configs.recommended,
  ...svelte.configs.recommended,
  { languageOptions: { globals: globals.browser } },
  { files: ['src/sw.js'], languageOptions: { globals: globals.serviceworker } },
  { files: ['test/**', '*.config.js'], languageOptions: { globals: globals.node } },
  {
    rules: {
      // `_` и `({ source, ...rest }) => rest` — нарочно отброшенные значения.
      'no-unused-vars': ['error', { varsIgnorePattern: '^_', argsIgnorePattern: '^_', ignoreRestSiblings: true }],
      // Ключ меняет то, как Svelte переиспользует строки списка, а в очереди
      // и плейлистах один трек бывает дважды: ключи ставим там, где нужны.
      'svelte/require-each-key': 'off',
      // {', '} и {' · '} в разметке — так пробел вокруг не теряется.
      'svelte/no-useless-mustaches': 'off',
      // `$: isPlaying = (kind, id) => …` — функция, зависящая от реактивных
      // значений; в синтаксисе до рун это обычный способ.
      'svelte/no-reactive-functions': 'off',
      // Считает зависимостью и переменную, которую блок только присваивает
      // (App.svelte: me) — ложные срабатывания.
      'svelte/infinite-reactive-loop': 'off',
    },
  },
  {
    // Правило не знает про `$:` — присваивание там читает шаблон.
    files: ['**/*.svelte'],
    rules: { 'no-useless-assignment': 'off' },
  },
];
