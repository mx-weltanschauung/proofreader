import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import globals from 'globals';

export default tseslint.config(
  { ignores: ['dist/**', 'coverage/**'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  // Именно configs.flat[...]: configs['recommended-latest'] в react-hooks 7
  // отдаётся в старом формате (plugins массивом строк), и ESLint 10 падает
  // на нём с сообщением про «"plugins" key defined as an array of strings»,
  // не называя виновника.
  reactHooks.configs.flat['recommended-latest'],
  {
    files: ['**/*.{ts,tsx}'],
    linterOptions: {
      reportUnusedDisableDirectives: 'error',
    },
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: { 'react-refresh': reactRefresh },
    rules: {
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      '@typescript-eslint/no-floating-promises': 'error',
      // checksVoidReturn.attributes обязателен: без него правило даёт 20
      // находок вместо одной, и 19 из них — onClick={async () => …} в JSX,
      // то есть штатный React, а не ошибка.
      '@typescript-eslint/no-misused-promises': [
        'error',
        { checksVoidReturn: { attributes: false } },
      ],
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
    },
  },
  {
    files: ['vite.config.ts', 'vitest.config.ts'],
    languageOptions: { globals: globals.node },
  },
  {
    // Служебные node-скрипты (замерщик вёрстки и подобные) — не браузерный код.
    files: ['scripts/**/*.mjs'],
    languageOptions: { globals: globals.node },
  },
);
