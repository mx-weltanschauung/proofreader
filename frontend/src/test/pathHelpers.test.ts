import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const SRC = join(__dirname, '..');
// utils/paths.ts (и его тест) строят адреса читальни. services/api.ts — это
// клиент REST API: baseURL уже несёт `/api`, поэтому в его литералах нет
// префикса `/api/`, и наивный образец задевает его наравне с читальней —
// хотя адрес API строго числовой и слага не несёт по определению.
const ALLOWED = ['utils/paths.ts', 'utils/paths.test.ts', 'services/api.ts'];

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) return sources(full);
    return /\.tsx?$/.test(full) ? [full] : [];
  });
}

// 87 мест построения адресов вручную были переведены на utils/paths.ts. Без
// ратчета они отрастают заново по одному, и первый же забытый даёт адрес без
// слага — рабочий, но неканоничный, то есть невидимый глазом.
describe('адреса строятся построителями', () => {
  it('шаблон пути работы или собрания не встречается вне utils/paths.ts', () => {
    const pattern = /['"`]\/(works|editions)\/\$\{/;
    const offenders = sources(SRC)
      .filter((f) => !ALLOWED.some((a) => f.endsWith(a)))
      .filter((f) => !f.endsWith('.test.ts') && !f.endsWith('.test.tsx'))
      .filter((f) => pattern.test(readFileSync(f, 'utf8')))
      .map((f) => f.slice(SRC.length + 1));
    expect(offenders).toEqual([]);
  });
});
