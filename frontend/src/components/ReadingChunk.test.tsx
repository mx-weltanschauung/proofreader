import { useEffect } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { ReadingChunk } from './ReadingChunk';
import type { ReadingPage } from '../types';

// @ts-expect-error - auto-render doesn't have type definitions
import renderMathInElement from 'katex/dist/contrib/auto-render';

vi.mock('katex/dist/contrib/auto-render', () => ({ default: vi.fn() }));

// useNoteXrefs и useFootnotePreview заменены на подставки, которые не делают
// ничего кроме учёта их собственного эффекта на переданном массиве
// зависимостей: настоящая реализация ничего не выполняет без реальных ссылок
// на примечания/сноски в разметке, а нам важно именно повторное срабатывание
// эффекта — тот же вопрос, что и у KaTeX-эффекта ниже. Обёртка над реальным
// useEffect сохраняет семантику deps: при [] эффект отработает один раз, при
// изменяющемся значении — на каждый рендер, и именно это должна ловить
// мутация зависимостей.
const { noteXrefsEffectSpy, footnotePreviewEffectSpy } = vi.hoisted(() => ({
  noteXrefsEffectSpy: vi.fn(),
  footnotePreviewEffectSpy: vi.fn(),
}));

vi.mock('../hooks/useNoteXrefs', () => ({
  useNoteXrefs: function useNoteXrefs(
    _containerRef: unknown,
    _workId: number | undefined,
    _workSlug: string | undefined,
    deps: unknown[],
  ) {
    useEffect(() => {
      noteXrefsEffectSpy();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, deps);
  },
}));

vi.mock('../hooks/useFootnotePreview', () => ({
  useFootnotePreview: function useFootnotePreview(_containerRef: unknown, deps: unknown[]) {
    useEffect(() => {
      footnotePreviewEffectSpy();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, deps);
  },
}));

const work = { id: 47, page_offset: 0 };

function page(number: number, overrides: Partial<ReadingPage> = {}): ReadingPage {
  return {
    page_number: number,
    html: `<p>Текст страницы ${number}</p>`,
    notes_html: '',
    blank: false,
    ...overrides,
  };
}

function renderChunk(pages: ReadingPage[], showPageInfo = false) {
  return render(
    <MemoryRouter>
      <ReadingChunk
        pages={pages}
        work={work}
        pageHref={(n) => `/works/47/pages/${n}`}
        showPageInfo={showPageInfo}
      />
    </MemoryRouter>,
  );
}

describe('ReadingChunk', () => {
  it('рисует секцию на страницу с якорем по общему договору', () => {
    const { container } = renderChunk([page(43), page(44)]);

    expect(container.querySelector('#chapter-page-43')).not.toBeNull();
    expect(container.querySelector('#chapter-page-44')).not.toBeNull();
    expect(screen.getByText('Текст страницы 43')).toBeInTheDocument();
  });

  // Поток читают подряд, и стык полос там виден так же, как в главе.
  it('склеивает оборванную на стыке полос фразу в один абзац', () => {
    const { container } = renderChunk([
      page(43, { html: '<p>то очевидно, что я</p>' }),
      page(44, { html: '<p>никогда бы не решился.</p>' }),
    ]);

    const paragraphs = Array.from(container.querySelectorAll('.page-html-content p'));
    expect(paragraphs).toHaveLength(1);

    const text = paragraphs[0].cloneNode(true) as HTMLElement;
    text.querySelectorAll('.page-marker').forEach((m) => m.remove());
    expect(text.textContent).toBe('то очевидно, что я никогда бы не решился.');
    expect(container.querySelectorAll('#chapter-page-44')).toHaveLength(1);
  });

  it('секции несут класс, по которому их находит useVisiblePage', () => {
    const { container } = renderChunk([page(43)]);

    const section = container.querySelector('#chapter-page-43');
    expect(section?.classList.contains('chapter-page-section')).toBe(true);
  });

  it('пустая страница не получает маркера номера', () => {
    const { container } = renderChunk([page(43, { blank: true, html: '' })]);

    expect(container.querySelector('#chapter-page-43')).not.toBeNull();
    expect(container.querySelector('.page-marker')).toBeNull();
  });

  it('непустая страница получает ссылку-маркер на саму страницу', () => {
    renderChunk([page(43)]);

    const marker = screen.getByRole('link', { name: /Страница 43/ });
    expect(marker).toHaveAttribute('href', '/works/47/pages/43');
  });

  it('сноски страницы рисуются сразу за её текстом', () => {
    const { container } = renderChunk([
      page(43, { notes_html: '<ol><li id="fn:43-r1">сноска</li></ol>' }),
      page(44),
    ]);

    const section = container.querySelector('#chapter-page-43');
    expect(section?.querySelector('#fn\\:43-r1')).not.toBeNull();
  });

  // Разметку сносок отдаёт RenderNotes (pkg/markdown/notes.go): <div
  // class="footnotes"> с <ul class="fn-list"> внутри. Всё её оформление —
  // кегль заголовка, список без буллетов, отбивка обратной ссылки,
  // scroll-margin-top под липкой панелью — написано в ReadingSurface.css от
  // .chapter-footnotes. Без этого класса поток показывал бы браузерный
  // default: заголовок крупнее основного текста и буллеты за колонкой.
  it('обёртка сносок несёт класс оформления сносок читалки', () => {
    const { container } = renderChunk([
      page(43, { notes_html: '<div class="footnotes"><ul class="fn-list"></ul></div>' }),
    ]);

    const notes = container.querySelector('.reading-chunk-notes');
    expect(notes?.classList.contains('chapter-footnotes')).toBe(true);
  });

  // Всё оформление маркера — колонка в отбивке, кегль, выравнивание цифр —
  // написано в ReadingSurface.css от .chapter-pages-content.show-page-info.
  // Без этого класса маркеры остаются скрытыми правилом display:none.
  it('с включёнными номерами корень несёт класс показа маркеров', () => {
    const { container } = renderChunk([page(43)], true);

    expect(container.querySelector('.reading-chunk')).toHaveClass('show-page-info');
  });

  it('с выключенными номерами класса показа нет', () => {
    const { container } = renderChunk([page(43)], false);

    expect(container.querySelector('.reading-chunk')).not.toHaveClass('show-page-info');
  });

  it('корень несёт класс типографики читалки вдобавок к своему', () => {
    // .chapter-pages-content — общий с ReadingSurface класс: шрифт, кегль,
    // отбивка абзацев и правило display:none на .page-marker живут в
    // ReadingSurface.css и рассчитаны на этот класс, а не на .reading-chunk.
    const { container } = renderChunk([page(43)]);

    const root = container.querySelector('.reading-chunk');
    expect(root?.classList.contains('chapter-pages-content')).toBe(true);
  });
});

describe('ReadingChunk: эффекты не растекаются на соседей', () => {
  it('второе окно не перевешивает эффекты первого', () => {
    const renderMath = vi.mocked(renderMathInElement);
    renderMath.mockClear();
    noteXrefsEffectSpy.mockClear();
    footnotePreviewEffectSpy.mockClear();

    const { rerender } = render(
      <MemoryRouter>
        <ReadingChunk
          pages={[page(43)]}
          work={work}
          pageHref={(n) => `/p/${n}`}
          showPageInfo={false}
        />
      </MemoryRouter>,
    );
    expect(renderMath).toHaveBeenCalledTimes(1);
    expect(noteXrefsEffectSpy).toHaveBeenCalledTimes(1);
    expect(footnotePreviewEffectSpy).toHaveBeenCalledTimes(1);

    // Появление второго окна рядом не должно заставлять первое
    // переобрабатываться: именно на этом старая читалка давала O(n²).
    rerender(
      <MemoryRouter>
        <ReadingChunk
          pages={[page(43)]}
          work={work}
          pageHref={(n) => `/p/${n}`}
          showPageInfo={false}
        />
        <ReadingChunk
          pages={[page(53)]}
          work={work}
          pageHref={(n) => `/p/${n}`}
          showPageInfo={false}
        />
      </MemoryRouter>,
    );

    // Ровно один новый вызов на каждый из трёх эффектов — на новое окно, и ни
    // одного повторного на старое.
    expect(renderMath).toHaveBeenCalledTimes(2);
    expect(noteXrefsEffectSpy).toHaveBeenCalledTimes(2);
    expect(footnotePreviewEffectSpy).toHaveBeenCalledTimes(2);
  });
});
