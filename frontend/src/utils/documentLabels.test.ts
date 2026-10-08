import { describe, it, expect } from 'vitest';
import { documentStateKind, documentStateLabel } from './documentLabels';

// M7. Перечень состояний называл ПЯТЬ сочетаний, а есть шестое: разбор
// опубликован, автор сохранил правку, но на проверку её не отправил.
// `review_status` при этом остаётся «одобрено», и полоса печатала «Разбор на
// людях», хотя на людях прежний текст — ровно то различие, ради которого
// затеяна вся ветка, автору и не показывали.
describe('documentStateKind — шестое состояние', () => {
  const live = {
    published_at: '2026-09-19T00:00:00Z',
    was_published: true,
    review_status: 'одобрено',
    title: 'Разбор',
    markdown_content: 'тело',
    published_title: 'Разбор',
    published_markdown: 'тело',
  };

  it('опубликованный без правок — просто «на людях»', () => {
    expect(documentStateKind(live)).toBe('live');
    expect(documentStateLabel(live)).toBe('Разбор на людях.');
  });

  it('сохранённая, но не отправленная правка поверх публикации — отдельное состояние', () => {
    const edited = { ...live, markdown_content: 'новое тело' };
    expect(documentStateKind(edited)).toBe('edited-over-live');
    expect(documentStateLabel(edited)).toMatch(/не отправлена/i);
    expect(documentStateLabel(edited)).toMatch(/прежняя редакция/i);

    // Заглавие считается наравне с телом: правка одного заглавия — та же
    // расходящаяся редакция.
    expect(documentStateKind({ ...live, title: 'Другое заглавие' })).toBe('edited-over-live');
  });

  it('отправленная правка перевешивает сохранённую: очередь важнее', () => {
    expect(
      documentStateKind({
        ...live,
        review_status: 'на_рассмотрении',
        markdown_content: 'новое тело',
      }),
    ).toBe('pending-over-live');
  });

  it('готовый признак с сервера в приоритете над сравнением редакций', () => {
    // Страница просмотра (`/view`) обеих редакций не везёт вовсе — только
    // готовый has_unpublished_changes.
    expect(
      documentStateKind({
        published_at: '2026-09-19T00:00:00Z',
        was_published: true,
        review_status: 'одобрено',
        has_unpublished_changes: true,
      }),
    ).toBe('edited-over-live');
    expect(
      documentStateKind({
        published_at: '2026-09-19T00:00:00Z',
        was_published: true,
        review_status: 'одобрено',
        has_unpublished_changes: false,
      }),
    ).toBe('live');
  });

  it('постороннему, которому сервер погасил published_*, правка не мерещится', () => {
    // documentForViewer подменяет title/markdown_content опубликованной
    // редакцией и гасит published_* до пустых строк. Сравнение без этой
    // оговорки печатало бы «есть правка» на КАЖДОМ опубликованном разборе.
    expect(
      documentStateKind({
        published_at: '2026-09-19T00:00:00Z',
        was_published: true,
        review_status: '',
        title: 'Разбор',
        markdown_content: 'тело',
        published_title: '',
        published_markdown: '',
      }),
    ).toBe('live');
  });

  it('прежние пять состояний не сдвинулись', () => {
    expect(
      documentStateKind({ published_at: null, was_published: true, review_status: 'одобрено' }),
    ).toBe('taken');
    expect(
      documentStateKind({
        published_at: null,
        was_published: false,
        review_status: 'на_рассмотрении',
      }),
    ).toBe('pending');
    expect(
      documentStateKind({ published_at: null, was_published: false, review_status: 'черновик' }),
    ).toBe('draft');
  });
});
