import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import type { ConceptEntry } from '../types';
import type { ConceptFragmentsResult } from '../hooks/useConceptFragments';
import { ConceptFragmentStream } from './ConceptFragmentStream';

// jsdom не знает IntersectionObserver. Подставной запоминает наблюдаемый узел
// и даёт тесту сообщить о его появлении в кадре.
class FakeObserver {
  static instances: FakeObserver[] = [];
  callback: IntersectionObserverCallback;
  observed: Element[] = [];

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback;
    FakeObserver.instances.push(this);
  }
  observe(el: Element) {
    this.observed.push(el);
  }
  unobserve() {}
  disconnect() {}
  enter() {
    this.callback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
}

function entry(referenceId: number): ConceptEntry {
  return {
    reference_id: referenceId,
    volume_number: 12,
    printed_start: 730,
    printed_end: 730,
    work_id: 14,
    work_title: 'Экономические рукописи',
    chapter_title: 'Введение',
    chapter_id: null,
    rubric: 'определение',
    rubric_path: ['определение'],
    is_uncertain: false,
    state: 'fragment',
    pages: [
      {
        page_id: 9000 + referenceId,
        page_number: 730,
        printed_page: 730,
        page_status: 'вычитана',
      },
    ],
    cuts: [
      {
        id: 91,
        bounds: {
          start_page_id: 9000 + referenceId,
          start_offset: 0,
          end_page_id: 9000 + referenceId,
          end_offset: 20,
        },
        status: 'machine',
        head_quote: `запись ${referenceId}`,
        parts: [
          {
            page_id: 9000 + referenceId,
            page_number: 730,
            printed_page: 730,
            page_status: 'вычитана',
            html: `<p>запись ${referenceId}</p>`,
          },
        ],
      },
    ],
    stale_cuts: [],
  };
}

function entryOf(referenceId: number, rubric: string): ConceptEntry {
  return { ...entry(referenceId), rubric, rubric_path: rubric === '' ? [] : [rubric] };
}

/** Запись с путём подрубрик; плоский rubric — лист пути, как шлёт сервер. */
function entryAt(referenceId: number, path: string[]): ConceptEntry {
  return { ...entry(referenceId), rubric: path[path.length - 1] ?? '', rubric_path: path };
}

function stream(over: Partial<ConceptFragmentsResult> = {}): ConceptFragmentsResult {
  return {
    entries: [entry(473)],
    total: 1,
    isLoading: false,
    isLoadingMore: false,
    error: '',
    hasMore: false,
    loadMore: vi.fn(),
    retry: vi.fn(),
    replaceEntry: vi.fn(),
    ...over,
  };
}

function renderStream(result: ConceptFragmentsResult, order: 'rubric' | 'page' = 'rubric') {
  const onOrderChange = vi.fn();
  const view = render(
    <MemoryRouter>
      <ConceptFragmentStream
        stream={result}
        slug="abstraktnyj-trud"
        order={order}
        onOrderChange={onOrderChange}
      />
    </MemoryRouter>,
  );
  return { ...view, onOrderChange };
}

describe('ConceptFragmentStream', () => {
  beforeEach(() => {
    FakeObserver.instances = [];
    vi.stubGlobal('IntersectionObserver', FakeObserver);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('рисует записи потока', () => {
    const { container } = renderStream(stream());
    expect(container.textContent).toContain('запись 473');
  });

  it('на загрузке первой порции показывает состояние ожидания', () => {
    renderStream(stream({ entries: [], total: 0, isLoading: true }));
    expect(screen.getByText(/Загруж/i)).toBeTruthy();
  });

  it('без записей объясняет, почему пусто', () => {
    renderStream(stream({ entries: [], total: 0 }));
    expect(screen.getByText(/ни один том/i)).toBeTruthy();
  });

  it('просит следующую порцию, когда сентинел попал в кадр', () => {
    const loadMore = vi.fn();
    renderStream(stream({ hasMore: true, total: 5, loadMore }));

    FakeObserver.instances[0].enter();

    expect(loadMore).toHaveBeenCalledTimes(1);
  });

  it('ошибка не прячет загруженное и даёт повтор', () => {
    const retry = vi.fn();
    const { container } = renderStream(stream({ error: 'сеть отвалилась', retry }));

    expect(container.textContent).toContain('запись 473');
    expect(screen.getByText('сеть отвалилась')).toBeTruthy();
    screen.getByRole('button', { name: /повторить/i }).click();
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it('тумблер порядка показывает текущий режим и сообщает о смене', async () => {
    const { onOrderChange } = renderStream(stream());

    const byRubric = screen.getByRole('button', { name: 'по рубрикам' });
    const byPage = screen.getByRole('button', { name: 'по томам' });
    expect(byRubric.getAttribute('aria-pressed')).toBe('true');
    expect(byPage.getAttribute('aria-pressed')).toBe('false');

    await userEvent.click(byPage);
    expect(onOrderChange).toHaveBeenCalledWith('page');
  });

  it('клик по уже активной кнопке тумблера ничего не сообщает', async () => {
    // Активная кнопка disabled — три клика по ней не должны класть в историю
    // три одинаковых записи (react-router push не схлопывает совпадающий URL).
    const { onOrderChange } = renderStream(stream());

    const byRubric = screen.getByRole('button', { name: 'по рубрикам' });
    expect(byRubric).toBeDisabled();

    await userEvent.click(byRubric);
    expect(onOrderChange).not.toHaveBeenCalled();
  });

  it('в порядке по рубрикам заголовок рисуется на смене подрубрики и не повторяется внутри группы', () => {
    const { container } = renderStream(
      stream({
        entries: [
          entryOf(473, 'определение'),
          entryOf(474, 'определение'),
          entryOf(520, 'как субстанция стоимости'),
        ],
        total: 3,
      }),
    );

    const headings = screen.getAllByRole('heading', { level: 2 });
    expect(headings.map((h) => h.textContent)).toEqual(['определение', 'как субстанция стоимости']);
    // Подрубрику несёт заголовок группы — шапка каждой записи её не дублирует.
    expect(container.querySelectorAll('.concept-fragment-rubrics')).toHaveLength(0);
  });

  it('в порядке по томам заголовков групп нет, подрубрика остаётся в шапке записи', () => {
    const { container } = renderStream(
      stream({
        entries: [entryOf(473, 'определение'), entryOf(520, 'как субстанция стоимости')],
        total: 2,
      }),
      'page',
    );

    expect(screen.queryAllByRole('heading', { level: 2 })).toHaveLength(0);
    expect(screen.getByText('как субстанция стоимости')).toBeTruthy();
    // Заголовков групп нет — подрубрику называет шапка каждой записи, по одной на запись.
    expect(container.querySelectorAll('.concept-fragment-rubrics')).toHaveLength(2);
  });

  it('пустая подрубрика заголовка не получает', () => {
    renderStream(stream({ entries: [entryOf(473, '')], total: 1 }));

    expect(screen.queryAllByRole('heading', { level: 2 })).toHaveLength(0);
  });

  it('смена съезда при том же листе печатает заголовок съезда и листа заново', () => {
    renderStream(
      stream({
        entries: [
          entryAt(473, ['II съезд РСДРП', 'значение съезда']),
          entryAt(474, ['III съезд РСДРП', 'значение съезда']),
        ],
        total: 2,
      }),
    );

    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual([
      'II съезд РСДРП',
      'III съезд РСДРП',
    ]);
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'значение съезда',
      'значение съезда',
    ]);
  });

  it('в порядке по томам шапка записи называет путь целиком, а не лист', () => {
    renderStream(
      stream({ entries: [entryAt(473, ['II съезд РСДРП', 'значение съезда'])], total: 1 }),
      'page',
    );

    expect(screen.getByText('II съезд РСДРП — значение съезда')).toBeTruthy();
  });
});
