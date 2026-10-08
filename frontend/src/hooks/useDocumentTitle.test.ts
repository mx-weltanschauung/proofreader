import { describe, expect, it, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useDocumentTitle, SITE_NAME } from './useDocumentTitle';

describe('useDocumentTitle', () => {
  beforeEach(() => {
    document.title = SITE_NAME;
  });

  it('ставит заголовок вкладки с именем читальни', () => {
    renderHook(() => useDocumentTitle('Государство и революция'));
    expect(document.title).toBe(`Государство и революция — ${SITE_NAME}`);
  });

  // Пока данные летят, заголовка ещё нет. Ставить «undefined — Читальня»
  // нельзя: это увидят и читатель, и поисковик.
  it('не трогает заголовок, пока названия нет', () => {
    renderHook(() => useDocumentTitle(undefined));
    expect(document.title).toBe(SITE_NAME);
  });

  it('возвращает имя читальни при уходе со страницы', () => {
    const { unmount } = renderHook(() => useDocumentTitle('Апрельские тезисы'));
    unmount();
    expect(document.title).toBe(SITE_NAME);
  });

  // Имя читальни не должно дублироваться, если оно уже есть в названии.
  it('не приписывает имя дважды', () => {
    renderHook(() => useDocumentTitle(`Подборки — ${SITE_NAME}`));
    expect(document.title).toBe(`Подборки — ${SITE_NAME}`);
  });
});

// Заголовок вкладки обязан совпадать с тем, что печатает internal/seo —
// расхождение читается поисковиком как подмена содержимого (см. task-12).
// Литералы ниже — не выдуманные, а взяты дословно из тестов internal/seo, а
// формулы сборки строки — те же выражения, что стоят в самих страницах
// (WorkDetail.tsx, ChapterView.tsx, ConceptView.tsx, CollectionView.tsx).
// Сторож неидеальный: если сервер уедет, тест этого не заметит, — но он
// ловит более частый случай, уход клиента.
describe('согласие с internal/seo', () => {
  beforeEach(() => {
    document.title = SITE_NAME;
  });

  // internal/seo/render_volume_test.go, TestWorkDocIsCardWithChapterLinks:
  // "Полное собрание сочинений. Том 42 — В. И. Ленин — Читальня".
  it('карточка тома: название — автор', () => {
    const title = ['Полное собрание сочинений. Том 42', 'В. И. Ленин'].filter(Boolean).join(' — ');
    renderHook(() => useDocumentTitle(title));
    expect(document.title).toBe(`Полное собрание сочинений. Том 42 — В. И. Ленин — ${SITE_NAME}`);
  });

  // internal/seo/render_text_test.go, TestChapterDocCarriesFullText:
  // "Государство и революция — В. И. Ленин — Читальня".
  it('глава: название — автор', () => {
    const chapterTitle = 'Государство и революция';
    const author = 'В. И. Ленин';
    const title = author ? `${chapterTitle} — ${author}` : chapterTitle;
    renderHook(() => useDocumentTitle(title));
    expect(document.title).toBe(`Государство и революция — В. И. Ленин — ${SITE_NAME}`);
  });

  // internal/seo/render_index.go, Concept: "{title} — предметный указатель —
  // Читальня".
  it('понятие: название — предметный указатель', () => {
    renderHook(() => useDocumentTitle('Абстрактный труд — предметный указатель'));
    expect(document.title).toBe(`Абстрактный труд — предметный указатель — ${SITE_NAME}`);
  });

  // internal/seo/render_index.go, Collection: "{title} — подборка —
  // Читальня".
  it('подборка: название — подборка', () => {
    renderHook(() => useDocumentTitle('О государстве — подборка'));
    expect(document.title).toBe(`О государстве — подборка — ${SITE_NAME}`);
  });

  // F6 итогового ревью: ни один тест не брал том с ненулевым page_offset —
  // самый повторяющийся класс дефектов проекта. internal/seo/render_text.go,
  // ReadPage: "{title}, с. {printed} — Читальня", где printed = pageNumber +
  // PageOffset (render_text_test.go, TestReadPagePrintsOffsetPageNumber:
  // внутренний номер 3 при смещении 100 печатает «с. 103»). PageView.tsx
  // строит заголовок той же формулой: `${work.title}, с.
  // ${pageNumber + work.page_offset}`.
  it('полоса с ненулевым смещением: название, печатный номер', () => {
    const pageNumber = 3;
    const pageOffset = 100;
    renderHook(() => useDocumentTitle(`Том 7, с. ${pageNumber + pageOffset}`));
    expect(document.title).toBe(`Том 7, с. 103 — ${SITE_NAME}`);
  });
});
