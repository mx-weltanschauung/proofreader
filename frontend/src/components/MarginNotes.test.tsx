import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { MarginNotes } from './MarginNotes';
import { marginNotesApi } from '../services/api';
import type { MarginNote } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function note(over: Partial<MarginNote>): MarginNote {
  return {
    id: 1,
    work_id: 19,
    target: 'quote',
    chapter_id: null,
    chapter_title: '',
    start_page: 5,
    end_page: 5,
    page_missing: false,
    snapshot_start_page: 5,
    quote_key: 'пятой',
    quote_tail: 'полосы.',
    quote_text: 'пятой полосы.',
    body: 'мысль',
    created_at: '2026-10-29T00:00:00Z',
    updated_at: '2026-10-29T00:00:00Z',
    ...over,
  };
}

/** Две полосы в разметке ReadingSurface: секция с data-page и текст полосы. */
function surface(): HTMLElement {
  const root = document.createElement('div');
  root.innerHTML =
    '<div class="chapter-page-section" data-page="5">' +
    '<div class="page-html-content"><p>Текст пятой полосы.</p><p>Второй абзац пятой.</p></div>' +
    '</div>' +
    '<div class="chapter-page-section" data-page="6">' +
    '<div class="page-html-content"><p>Текст шестой полосы.</p></div>' +
    '</div>';
  document.body.append(root);
  return root;
}

function renderNotes(
  root: HTMLElement,
  notes: MarginNote[],
  deps: unknown[] = [1],
  handlers: { onChange?: (n: MarginNote) => void; onDelete?: (id: number) => void } = {},
) {
  const onChange = handlers.onChange ?? vi.fn();
  const onDelete = handlers.onDelete ?? vi.fn();
  // Реф один на все перерисовки, как у настоящей поверхности (useRef): новый
  // объект на каждую перерисовку перезапускал бы слой целиком, и тест смены
  // deps проверял бы не то.
  const contentRef = { current: root };
  const ui = (d: unknown[]) => (
    <MemoryRouter>
      <MarginNotes
        contentRef={contentRef}
        notes={notes}
        pageNumbers={[5, 6]}
        deps={d}
        onChange={onChange}
        onDelete={onDelete}
      />
    </MemoryRouter>
  );
  const utils = render(ui(deps));
  return { ...utils, rerenderDeps: (d: unknown[]) => utils.rerender(ui(d)), onChange, onDelete };
}

describe('MarginNotes', () => {
  it('удаляет прежние тексты сразу, до отложенной раскладки следующего списка', async () => {
    const root = surface();
    const contentRef = { current: root };
    const previous = note({ id: -9, body: 'Секрет прежней сессии' });
    const ui = (notes: MarginNote[], custom: boolean) => (
      <MemoryRouter>
        <MarginNotes
          contentRef={contentRef}
          notes={notes}
          pageNumbers={[5, 6]}
          deps={[1]}
          onChange={vi.fn()}
          onDelete={vi.fn()}
          renderCard={() => (custom ? <span>Секрет прежней сессии</span> : null)}
        />
      </MemoryRouter>
    );
    const { rerender } = render(ui([previous], true));
    expect(await screen.findByText('Секрет прежней сессии')).toBeInTheDocument();
    rerender(ui([], false));
    expect(screen.queryByText('Секрет прежней сессии')).toBeNull();
  });
  beforeEach(() => {
    document.body.innerHTML = '';
  });
  afterEach(() => vi.restoreAllMocks());

  it('подсвечивает цитату заметки меткой с её id', async () => {
    const root = surface();
    renderNotes(root, [note({ id: 3 })]);

    await waitFor(() => expect(root.querySelector('mark.margin-hit')).not.toBeNull());
    const mark = root.querySelector('mark.margin-hit') as HTMLElement;
    expect(mark.dataset.noteIds).toBe('3');
    expect(mark.textContent).toBe('пятой полосы.');
    // Найденная заметка видна карточкой, без пометки о потере места.
    expect(await screen.findByText('мысль')).toBeInTheDocument();
    expect(screen.queryByText('Место в тексте не найдено')).toBeNull();
  });

  it('ключ не нашёлся — карточка в начале своей полосы со снимком цитаты', async () => {
    const root = surface();
    renderNotes(root, [
      note({ id: 4, start_page: 6, quote_key: 'исчезнувший ключ', quote_text: 'снимок цитаты' }),
    ]);

    const label = await screen.findByText('Место в тексте не найдено');
    const section = root.querySelector('[data-page="6"]') as HTMLElement;
    expect(section.contains(label)).toBe(true);
    expect(within(section).getByText(/снимок цитаты/)).toBeInTheDocument();
    expect(root.querySelector('mark.margin-hit')).toBeNull();
  });

  it('полоса пропала (page_missing) — карточка по снимку номера полосы', async () => {
    const root = surface();
    renderNotes(root, [
      note({
        id: 5,
        start_page: null,
        end_page: null,
        page_missing: true,
        snapshot_start_page: 6,
        quote_text: 'старый снимок',
      }),
    ]);

    const label = await screen.findByText('Место в тексте не найдено');
    const section = root.querySelector('[data-page="6"]') as HTMLElement;
    expect(section.contains(label)).toBe(true);
    expect(within(section).getByText(/старый снимок/)).toBeInTheDocument();
  });

  it('полоса пропала, а под тем же номером цитата есть — подсвечена без пометки о потере', async () => {
    const root = surface();
    renderNotes(root, [
      note({
        id: 7,
        start_page: null,
        end_page: null,
        page_missing: true,
        snapshot_start_page: 5,
      }),
    ]);

    await waitFor(() => expect(root.querySelector('mark.margin-hit')).not.toBeNull());
    expect((root.querySelector('mark.margin-hit') as HTMLElement).dataset.noteIds).toBe('7');
    expect(await screen.findByText('мысль')).toBeInTheDocument();
    expect(screen.queryByText('Место в тексте не найдено')).toBeNull();
  });

  it('цитата в блоке сносок полосы — карточка в начале полосы, а не пропадает', async () => {
    const root = document.createElement('div');
    root.innerHTML =
      '<div class="chapter-page-section" data-page="5">' +
      '<div class="page-html-content"><p>Текст пятой полосы.</p></div>' +
      '<div class="reading-chunk-notes"><ol><li>Сноска о редком издании.</li></ol></div>' +
      '</div>';
    document.body.append(root);
    renderNotes(root, [
      note({ id: 8, quote_key: 'редком', quote_tail: 'издании.', body: 'о сноске' }),
    ]);

    const body = await screen.findByText('о сноске');
    expect(root.querySelector('mark.margin-hit')?.textContent).toBe('редком издании.');
    const content = root.querySelector('.page-html-content') as HTMLElement;
    expect(content.firstElementChild?.classList.contains('margin-note-host')).toBe(true);
    expect(content.firstElementChild?.contains(body)).toBe(true);
  });

  it('доступное имя карточки обрезано, а не весь текст заметки', async () => {
    const root = surface();
    const long = 'слово '.repeat(400).trim();
    renderNotes(root, [note({ id: 3, body: long })]);
    const button = await screen.findByTitle('Изменить заметку');
    const label = button.getAttribute('aria-label') ?? '';
    expect(Array.from(label)).toHaveLength(120);
    expect(label.endsWith('…')).toBe(true);
    expect(long.startsWith(label.slice(0, -1))).toBe(true);
  });

  it('заметка чужой полосы не рисуется', async () => {
    const root = surface();
    renderNotes(root, [note({ id: 6, start_page: 40, quote_key: 'пятой', body: 'далёкая' })]);
    // Даём слою отработать и убеждаемся, что ничего не появилось.
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByText('далёкая')).toBeNull();
    expect(root.querySelector('mark.margin-hit')).toBeNull();
  });

  it('смена deps не удваивает подсветку', async () => {
    const root = surface();
    const { rerenderDeps } = renderNotes(root, [note({ id: 3 })], [1]);
    await waitFor(() => expect(root.querySelectorAll('mark.margin-hit')).toHaveLength(1));

    rerenderDeps([2]);
    rerenderDeps([3]);
    await new Promise((r) => setTimeout(r, 0));

    expect(root.querySelectorAll('mark.margin-hit')).toHaveLength(1);
    expect(root.querySelector('mark.margin-hit mark')).toBeNull();
    expect(screen.getAllByText('мысль')).toHaveLength(1);
  });

  it('текст заметки выводится буквально, без разметки', async () => {
    const root = surface();
    renderNotes(root, [note({ id: 3, body: '<b>x</b>' })]);
    const body = await screen.findByText('<b>x</b>');
    expect(body.querySelector('b')).toBeNull();
    expect(document.querySelector('.margin-note-card b')).toBeNull();
  });

  it('текст карточки не входит в текст полосы для цитирования', async () => {
    const root = surface();
    renderNotes(root, [note({ id: 3, body: 'пятой' })]);
    await screen.findByText('пятой', { selector: '.margin-note-card *' });
    const { flattenText } = await import('../utils/quoteMatch');
    expect(flattenText(root).text).toBe(
      'Текст пятой полосы.Второй абзац пятой.Текст шестой полосы.',
    );
  });

  it('нажатие на подсветку раскрывает правку, сохранение зовёт onChange', async () => {
    const root = surface();
    const updated = note({ id: 3, body: 'новая мысль' });
    const update = vi.spyOn(marginNotesApi, 'update').mockImplementation(() => ok(updated));
    const { onChange } = renderNotes(root, [note({ id: 3 })]);

    await waitFor(() => expect(root.querySelector('mark.margin-hit')).not.toBeNull());
    fireEvent.click(root.querySelector('mark.margin-hit')!);

    const field = await screen.findByRole('textbox', { name: 'Текст заметки' });
    expect(field).toHaveValue('мысль');
    fireEvent.change(field, { target: { value: 'новая мысль' } });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(update).toHaveBeenCalledWith(3, 'новая мысль'));
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(updated));
  });

  it('удаление спрашивает подтверждение и зовёт onDelete', async () => {
    const root = surface();
    const remove = vi.spyOn(marginNotesApi, 'remove').mockImplementation(() => ok(undefined));
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { onDelete } = renderNotes(root, [note({ id: 3 })]);

    fireEvent.click(await screen.findByRole('button', { name: /мысль/ }));
    fireEvent.click(await screen.findByRole('button', { name: 'Удалить' }));

    expect(confirm).toHaveBeenCalledWith('Удалить заметку?');
    await waitFor(() => expect(remove).toHaveBeenCalledWith(3));
    await waitFor(() => expect(onDelete).toHaveBeenCalledWith(3));
  });

  it('отказ в подтверждении удаления ничего не удаляет', async () => {
    const root = surface();
    const remove = vi.spyOn(marginNotesApi, 'remove').mockImplementation(() => ok(undefined));
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    renderNotes(root, [note({ id: 3 })]);

    fireEvent.click(await screen.findByRole('button', { name: /мысль/ }));
    fireEvent.click(await screen.findByRole('button', { name: 'Удалить' }));
    expect(remove).not.toHaveBeenCalled();
  });
});
