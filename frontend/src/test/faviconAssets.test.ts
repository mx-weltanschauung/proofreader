import { describe, it, expect } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = join(__dirname, '..', '..');
const PUBLIC = join(ROOT, 'public');

/**
 * Ссылка на иконку ломается молча: браузер не пишет в консоль, тест на
 * компоненты о ней не знает, а во вкладке просто остаётся серый лист. Ровно
 * так `index.html` полгода ссылался на `/vite.svg`, которого в репозитории
 * никогда не было. Сторож сверяет заявленное с лежащим на диске.
 */
function hrefs(html: string, rel: RegExp): string[] {
  const out: string[] = [];
  for (const tag of html.match(/<link\b[^>]*>/g) ?? []) {
    const relAttr = tag.match(/\brel="([^"]*)"/)?.[1];
    const href = tag.match(/\bhref="([^"]*)"/)?.[1];
    if (relAttr && href && rel.test(relAttr)) out.push(href);
  }
  return out;
}

describe('иконки читальни', () => {
  const html = readFileSync(join(ROOT, 'index.html'), 'utf8');

  it('каждая иконка из index.html лежит в public/', () => {
    const declared = hrefs(html, /icon|manifest/);
    expect(declared.length).toBeGreaterThan(0);
    const missing = declared.filter((href) => !existsSync(join(PUBLIC, href.replace(/^\//, ''))));
    expect(missing).toEqual([]);
  });

  it('index.html заявляет svg, ico, apple-touch и манифест', () => {
    expect(hrefs(html, /^icon$/).some((h) => h.endsWith('.svg'))).toBe(true);
    expect(hrefs(html, /^icon$/).some((h) => h.endsWith('.ico'))).toBe(true);
    expect(hrefs(html, /apple-touch-icon/)).toHaveLength(1);
    expect(hrefs(html, /^manifest$/)).toHaveLength(1);
  });

  it('каждая иконка из манифеста лежит в public/', () => {
    const manifest = JSON.parse(readFileSync(join(PUBLIC, 'site.webmanifest'), 'utf8'));
    const missing = (manifest.icons as { src: string }[])
      .map((i) => i.src)
      .filter((src) => !existsSync(join(PUBLIC, src.replace(/^\//, ''))));
    expect(missing).toEqual([]);
  });

  // Расширения .webmanifest в mime.types nginx нет: без явного типа файл
  // уезжает как application/octet-stream. Поймано приёмкой на боевом, а не
  // тестом, — поэтому тест теперь есть.
  it('nginx отдаёт манифест верным типом', () => {
    const conf = readFileSync(join(ROOT, 'nginx.conf'), 'utf8');
    const block = conf.match(/location = \/site\.webmanifest \{[^}]*\}[^}]*\}/s)?.[0] ?? '';
    expect(block).toContain('application/manifest+json');
  });

  // Андроид обрезает иконку по своей маске и оставляет от квадрата круг:
  // без maskable-варианта с запасом по краям рамка плитки уезжает под нож.
  it('манифест объявляет maskable-иконку', () => {
    const manifest = JSON.parse(readFileSync(join(PUBLIC, 'site.webmanifest'), 'utf8'));
    const purposes = (manifest.icons as { purpose?: string }[]).map((i) => i.purpose ?? 'any');
    expect(purposes.some((p) => p.split(/\s+/).includes('maskable'))).toBe(true);
  });
});
