import { describe, it, expect } from 'vitest';
import type { Edition, EventOrganizationSummary, VolumeSummary } from '../types';
import {
  DEFAULT_SECTION,
  buildHomeLayout,
  byHoldings,
  editionAnchor,
  organizationAnchor,
  shortEditionTitle,
  splitAuthor,
} from './homeLayout';

function volume(id: number, over: Partial<VolumeSummary> = {}): VolumeSummary {
  return {
    id,
    title: `том ${id}`,
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '2026-01-01',
    volume_number: id,
    pages_total: 100,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

function vols(n: number, pages = 100): VolumeSummary[] {
  return Array.from({ length: n }, (_, i) => volume(i + 1, { pages_total: pages }));
}

function edition(id: number, title: string, shelf_group?: string): Edition {
  return {
    id,
    title,
    slug: `ed-${id}`,
    url_slug: `e${id}`,
    description: '',
    shelf_group,
    created_at: '',
    updated_at: '',
  };
}

function org(id: number, shelf_group?: string): EventOrganizationSummary {
  return { id, slug: `org-${id}`, title: `Организация ${id}`, events_total: 3, shelf_group };
}

describe('buildHomeLayout', () => {
  it('пустое поле — раздел по умолчанию, и он первый', () => {
    const layout = buildHomeLayout(
      [
        { edition: edition(1, 'А. Автор. Сочинения', ''), volumes: vols(2) },
        { edition: edition(2, 'Раздел Б. Отчёты', 'Раздел Б'), volumes: vols(30) },
      ],
      [],
      [],
      [],
    );
    expect(layout.sections.map((s) => s.name)).toEqual([DEFAULT_SECTION, 'Раздел Б']);
    expect(layout.sections.map((s) => s.anchor)).toEqual(['razdel-1', 'razdel-2']);
  });

  it('прочие разделы — по числу томов, затем по имени', () => {
    const layout = buildHomeLayout(
      [
        { edition: edition(1, 'X', 'Вега'), volumes: vols(3) },
        { edition: edition(2, 'Y', 'Альфа'), volumes: vols(3) },
        { edition: edition(3, 'Z', 'Бета'), volumes: vols(5) },
        { edition: edition(4, 'W', 'Вега'), volumes: vols(1) },
      ],
      [],
      [],
      [],
    );
    // Вега 4 тома, Бета 5, Альфа 3.
    expect(layout.sections.map((s) => s.name)).toEqual(['Бета', 'Вега', 'Альфа']);
  });

  it('имя раздела с пробелами по краям — тот же раздел', () => {
    const layout = buildHomeLayout(
      [
        { edition: edition(1, 'X', 'Раздел Б'), volumes: vols(1) },
        { edition: edition(2, 'Y', '  Раздел Б '), volumes: vols(1) },
      ],
      [],
      [],
      [],
    );
    expect(layout.sections).toHaveLength(1);
    expect(layout.sections[0].shelves).toHaveLength(2);
  });

  it('внутри раздела — byHoldings', () => {
    const layout = buildHomeLayout(
      [
        { edition: edition(1, 'Мало', ''), volumes: vols(2) },
        { edition: edition(2, 'Много', ''), volumes: vols(5) },
        { edition: edition(3, 'Толще', ''), volumes: vols(2, 900) },
      ],
      [],
      [],
      [],
    );
    expect(layout.sections[0].shelves.map((s) => s.edition.title)).toEqual(['Много', 'Толще', 'Мало']);
  });

  it('организация с полем — в своём разделе, без поля — в looseOrganizations', () => {
    const layout = buildHomeLayout(
      [{ edition: edition(1, 'Раздел Б. Отчёты', 'Раздел Б'), volumes: vols(1) }],
      [org(1, 'Раздел Б'), org(2, ''), org(3)],
      [],
      [],
    );
    expect(layout.sections[0].organizations.map((o) => o.id)).toEqual([1]);
    expect(layout.looseOrganizations.map((o) => o.id)).toEqual([2, 3]);
  });

  it('раздел только с организацией рисуется', () => {
    const layout = buildHomeLayout([], [org(1, 'Раздел Б')], [], []);
    expect(layout.sections.map((s) => s.name)).toEqual(['Раздел Б']);
    expect(layout.sections[0].shelves).toEqual([]);
  });

  it('раздел без полок и организаций не появляется', () => {
    const layout = buildHomeLayout([{ edition: edition(1, 'X', 'Раздел Б'), volumes: vols(1) }], [], [], []);
    expect(layout.sections.map((s) => s.name)).toEqual(['Раздел Б']);
    const empty = buildHomeLayout([], [], [], []);
    expect(empty.sections).toEqual([]);
  });

  it('полка несёт якорь и короткое заглавие своего раздела', () => {
    const layout = buildHomeLayout(
      [{ edition: edition(7, 'Раздел Б. Отчёты', 'Раздел Б'), volumes: vols(1) }],
      [],
      [],
      [],
    );
    expect(layout.sections[0].shelves[0]).toMatchObject({ anchor: 'ed-e7', shortTitle: 'Отчёты' });
  });
});

describe('byHoldings', () => {
  it('больше томов выше, при равенстве — больше страниц', () => {
    const a = { volumes: vols(2, 100) };
    const b = { volumes: vols(2, 300) };
    const c = { volumes: vols(3, 10) };
    expect([a, b, c].sort(byHoldings)).toEqual([c, b, a]);
  });
});

describe('shortEditionTitle', () => {
  it('срезает «{раздел}. »', () => {
    expect(shortEditionTitle('Раздел Б. Отчёты', 'Раздел Б')).toBe('Отчёты');
  });
  it('заглавие, равное разделу, остаётся целым', () => {
    expect(shortEditionTitle('Раздел Б', 'Раздел Б')).toBe('Раздел Б');
    expect(shortEditionTitle('Раздел Б.  ', 'Раздел Б')).toBe('Раздел Б.  ');
  });
  it('не режет посреди слова', () => {
    expect(shortEditionTitle('Раздел Бета. X', 'Раздел Б')).toBe('Раздел Бета. X');
  });
  it('раздел по умолчанию ничего не срезает', () => {
    expect(shortEditionTitle(`${DEFAULT_SECTION}. X`, DEFAULT_SECTION)).toBe(`${DEFAULT_SECTION}. X`);
  });
});

describe('splitAuthor', () => {
  it('пропускает инициалы', () => {
    expect(splitAuthor('Г. В. Ф. Гегель. Сочинения в 14 томах')).toEqual({
      author: 'Г. В. Ф. Гегель',
      rest: 'Сочинения в 14 томах',
    });
  });
  it('двое авторов', () => {
    expect(splitAuthor('К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.')).toEqual({
      author: 'К. Маркс и Ф. Энгельс',
      rest: 'Сочинения, 2-е изд.',
    });
  });
  it('не делится — целиком автор', () => {
    expect(splitAuthor('Стенографические отчёты')).toEqual({ author: 'Стенографические отчёты', rest: '' });
    expect(splitAuthor('Сочинения, 2-е изд.')).toEqual({ author: 'Сочинения, 2-е изд.', rest: '' });
  });
});

describe('якоря', () => {
  it('собрание — по адресному слагу, иначе по слагу', () => {
    expect(editionAnchor(edition(1, 'X'))).toBe('ed-e1');
    expect(editionAnchor({ ...edition(1, 'X'), url_slug: '' })).toBe('ed-ed-1');
  });
  it('организация — по слагу', () => {
    expect(organizationAnchor({ slug: 'org-a' })).toBe('org-org-a');
  });
});
