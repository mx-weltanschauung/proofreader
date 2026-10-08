import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const SRC = join(__dirname, '..');

function sourceFiles(dir: string, re: RegExp): string[] {
  return readdirSync(dir)
    .flatMap((entry) => {
      const full = join(dir, entry);
      if (statSync(full).isDirectory()) return sourceFiles(full, re);
      return re.test(full) ? [full] : [];
    })
    .sort();
}

// Кнопочный класс один на весь проект, и живёт он в index.css. Модификатор,
// написанный в разметке с опечаткой или придуманный на ходу, не сломает
// сборку и не выдаст себя в консоли: кнопка просто останется без стиля —
// ровно так на экране чтения оказалась .immersive-toggle, у которой не было
// ни одного правила CSS.
describe('кнопочная система', () => {
  const indexCss = readFileSync(join(SRC, 'index.css'), 'utf8');

  it('index.css объявляет базовый класс и состояние тумблера', () => {
    expect(indexCss).toMatch(/^\.btn\s*\{/m);
    expect(indexCss).toMatch(/\.btn\[aria-pressed='true'\]/);
  });

  it('каждый модификатор .btn-*, встречающийся в разметке, объявлен', () => {
    const declared = new Set([...indexCss.matchAll(/\.(btn-[a-z]+)/g)].map((m) => m[1]));

    const used = new Set<string>();
    for (const file of sourceFiles(SRC, /\.tsx$/)) {
      const content = readFileSync(file, 'utf8');
      for (const m of content.matchAll(/\bbtn-[a-z]+/g)) used.add(m[0]);
    }

    expect([...used].filter((cls) => !declared.has(cls)).sort()).toEqual([]);
  });
});
