import { describe, it, expect } from 'vitest';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { ESLint } from 'eslint';

const FRONTEND_ROOT = join(__dirname, '..', '..');

// Скрипт lint в этом проекте однажды уже пролежал нерабочим всю историю
// репозитория: зависимости стояли, а конфига в git не было ни одного. Падение
// было тихим — про него узнавали, только запустив линтер руками.
describe('конфигурация ESLint', () => {
  it('конфиг лежит на месте', () => {
    expect(existsSync(join(FRONTEND_ROOT, 'eslint.config.js'))).toBe(true);
  });

  it('ESLint считает конфиг для файла из src', async () => {
    const eslint = new ESLint({ cwd: FRONTEND_ROOT });
    const config = await eslint.calculateConfigForFile(join(FRONTEND_ROOT, 'src', 'App.tsx'));
    expect(config.rules).toBeDefined();
  });

  // Второй способ сломаться — не потерять конфиг, а обесценить его: выключить
  // мешающие правила ради зелёного прогона. Ровно это пришлось откатывать
  // в коммите 1396b21, где no-explicit-any, no-unused-vars и exhaustive-deps
  // разом стали 'off'.
  it('ключевые правила включены, а не выключены ради зелёного', async () => {
    const eslint = new ESLint({ cwd: FRONTEND_ROOT });
    const config = await eslint.calculateConfigForFile(join(FRONTEND_ROOT, 'src', 'App.tsx'));
    const required = [
      'react-hooks/rules-of-hooks',
      'react-hooks/exhaustive-deps',
      '@typescript-eslint/no-explicit-any',
      '@typescript-eslint/no-unused-vars',
      '@typescript-eslint/no-floating-promises',
    ];
    const disabled = required.filter((name) => {
      const entry = config.rules?.[name];
      if (entry === undefined) return true;
      const severity = Array.isArray(entry) ? entry[0] : entry;
      return severity === 'off' || severity === 0;
    });
    expect(disabled, `правила выключены или отсутствуют: ${disabled.join(', ')}`).toEqual([]);
  });
});
