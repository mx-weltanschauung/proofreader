import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConceptEntryExpand } from './ConceptEntryExpand';
import { conceptsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { ConceptCut } from '../types';

vi.mock('../services/api', () => ({
  conceptsApi: { expandPage: vi.fn(), putCuts: vi.fn() },
}));

// useAuth мокается по умолчанию как редактор; тест «без прав» переопределяет
// мок на неавторизованное состояние через mockReturnValue (не Once — за время
// загрузки страницы компонент перерисовывается несколько раз, и хук
// вызывается на каждой перерисовке).
vi.mock('../hooks/useAuth', () => ({
  useAuth: vi.fn(() => ({ user: { role: 'editor' }, isAuthenticated: true })),
}));

const expandPage = vi.mocked(conceptsApi.expandPage);
const putCuts = vi.mocked(conceptsApi.putCuts);
const mockedUseAuth = vi.mocked(useAuth);

/** Правимая вырезка: та же страница, что грузит `payload` (page_number 730). */
function editableCut(over: Partial<ConceptCut> = {}): ConceptCut {
  return {
    id: 91,
    bounds: { start_page_id: 9143, start_offset: 12, end_page_id: 9143, end_offset: 28 },
    status: 'machine',
    head_quote: 'Внутри вырезки.',
    parts: [
      {
        page_id: 9143,
        page_number: 730,
        printed_page: 730,
        page_status: 'не_вычитана',
        html: '<p>Внутри вырезки.</p>',
      },
    ],
    ...over,
  };
}

/** Чужая вырезка адреса: другая страница — редактор её не показывает, но обязан сохранить. */
function foreignCut(over: Partial<ConceptCut> = {}): ConceptCut {
  return {
    id: 92,
    bounds: { start_page_id: 9200, start_offset: 5, end_page_id: 9200, end_offset: 9 },
    status: 'machine',
    head_quote: 'Другое место.',
    parts: [
      {
        page_id: 9200,
        page_number: 745,
        printed_page: 745,
        page_status: 'не_вычитана',
        html: '<p>Другое место.</p>',
      },
    ],
    ...over,
  };
}

/**
 * Живая вырезка с исчезнувшими частями: якорные страницы вне диапазона
 * текущего адреса, `parts: []` (см. cutRanges), но `id`/`bounds` настоящие —
 * это не синтетическая вырезка. На клиенте нет номера страницы, чтобы описать
 * такую границами, поэтому она обязана уйти на сохранение ссылкой по id.
 */
function vanishedPartsCut(over: Partial<ConceptCut> = {}): ConceptCut {
  return {
    id: 93,
    bounds: { start_page_id: 9300, start_offset: 3, end_page_id: 9300, end_offset: 15 },
    status: 'machine',
    head_quote: '',
    parts: [],
    ...over,
  };
}

/**
 * Вырезка, у которой пропал ТОЛЬКО начальный якорь: cutRanges (см.
 * concept_cuts.go) при отсутствующей начальной странице оттягивает видимую
 * часть к первой доступной странице адреса, но bounds по-прежнему хранит
 * исходный start_page_id — той страницы, которой уже нет среди parts.
 * start_offset из bounds относится к пропавшей странице, а не к той, чей
 * номер вернул бы parts[0] — их нельзя смешивать в одних границах.
 */
function mismatchedStartAnchorCut(over: Partial<ConceptCut> = {}): ConceptCut {
  return {
    id: 94,
    bounds: { start_page_id: 9999, start_offset: 3, end_page_id: 9200, end_offset: 20 },
    status: 'machine',
    head_quote: 'Уцелевшая часть.',
    parts: [
      {
        page_id: 9200,
        page_number: 745,
        printed_page: 745,
        page_status: 'не_вычитана',
        html: '<p>Уцелевшая часть.</p>',
      },
    ],
    ...over,
  };
}

const payload = {
  data: {
    page_id: 9143,
    page_number: 730,
    printed_page: 730,
    page_status: 'не_вычитана',
    markdown: 'До вырезки. Внутри вырезки. После.',
    chunks: [
      { html: '<p>До вырезки. </p>', inside: false },
      { html: '<p>Внутри вырезки.</p>', inside: true },
      { html: '<p> После.</p>', inside: false },
    ],
  },
};

describe('ConceptEntryExpand', () => {
  beforeEach(() => {
    expandPage.mockReset();
    expandPage.mockResolvedValue(payload as never);
    putCuts.mockReset();
    putCuts.mockResolvedValue({ data: { state: 'fragment', cuts: [] } } as never);
  });

  afterEach(() => {
    // Возвращаем мок к состоянию редактора, чтобы правка мока в одном тесте
    // не просочилась в соседние (порядок тестов задавать не хочется).
    mockedUseAuth.mockReturnValue({ user: { role: 'editor' }, isAuthenticated: true } as never);
  });

  it('подсвечивает куски внутри вырезки', async () => {
    const { container } = render(
      <ConceptEntryExpand slug="abstraktnyj-trud" referenceId={473} pageId={9143} cuts={[]} />,
    );

    await waitFor(() => expect(container.querySelectorAll('.concept-cut-chunk')).toHaveLength(3));
    expect(container.querySelectorAll('.concept-cut-chunk-inside')).toHaveLength(1);
  });

  it('переключается в исходник и обратно', async () => {
    const user = userEvent.setup();
    const { container } = render(
      <ConceptEntryExpand slug="abstraktnyj-trud" referenceId={473} pageId={9143} cuts={[]} />,
    );
    await waitFor(() => expect(container.querySelector('.concept-cut-chunk')).not.toBeNull());

    await user.click(screen.getByRole('button', { name: /править границы/ }));
    expect(container.querySelector('pre.concept-cut-source')).not.toBeNull();

    await user.click(screen.getByRole('button', { name: /закончить правку/ }));
    expect(container.querySelector('pre.concept-cut-source')).toBeNull();
  });

  it('ошибка загрузки не роняет запись', async () => {
    expandPage.mockRejectedValueOnce(new Error('нет сети'));
    render(
      <ConceptEntryExpand slug="abstraktnyj-trud" referenceId={473} pageId={9143} cuts={[]} />,
    );

    await waitFor(() =>
      expect(screen.getByText(/Не удалось загрузить страницу/)).toBeInTheDocument(),
    );
  });

  it('читатель без прав не видит кнопку правки', async () => {
    mockedUseAuth.mockReturnValue({ user: null, isAuthenticated: false } as never);

    const { container } = render(
      <ConceptEntryExpand slug="abstraktnyj-trud" referenceId={473} pageId={9143} cuts={[]} />,
    );

    await waitFor(() => expect(container.querySelector('.concept-cut-chunk')).not.toBeNull());
    expect(screen.queryByRole('button', { name: /править границы/ })).toBeNull();
  });

  it('показывает состав набора до сохранения: свою страницу и чужие вырезки отдельно', async () => {
    const user = userEvent.setup();
    render(
      <ConceptEntryExpand
        slug="abstraktnyj-trud"
        referenceId={473}
        pageId={9143}
        cuts={[editableCut(), foreignCut()]}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
    );

    await user.click(screen.getByRole('button', { name: /править границы/ }));

    // Запись заменяющая — до нажатия «сохранить» человек должен видеть, что
    // уйдёт на сервер целиком, а не только то, что он видит на этой странице.
    expect(
      screen.getByText(/Сохранится 2 вырезок адреса: 1 этой страницы, 1 с других страниц/),
    ).toBeInTheDocument();
  });

  it('сохранение без правок не теряет чужую вырезку адреса и не трогает её статус', async () => {
    const user = userEvent.setup();
    render(
      <ConceptEntryExpand
        slug="abstraktnyj-trud"
        referenceId={473}
        pageId={9143}
        cuts={[editableCut(), foreignCut()]}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
    );

    await user.click(screen.getByRole('button', { name: /править границы/ }));
    await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

    await waitFor(() => expect(putCuts).toHaveBeenCalled());
    const [slugArg, refIdArg, body] = putCuts.mock.calls[0];
    expect(slugArg).toBe('abstraktnyj-trud');
    expect(refIdArg).toBe(473);
    expect(body.cuts).toHaveLength(2);
    // Чужая вырезка (страница 745) уходит как пришла — своим прежним
    // статусом machine, не confirmed только потому, что человек тронул
    // соседнюю вырезку на текущей странице.
    expect(body.cuts).toContainEqual({
      start_page: 745,
      start_offset: 5,
      end_page: 745,
      end_offset: 9,
      status: 'machine',
    });
    expect(body.cuts).toContainEqual({
      start_page: 730,
      start_offset: 12,
      end_page: 730,
      end_offset: 28,
      status: 'confirmed',
    });
  });

  it('сохранение не теряет вырезку с исчезнувшими частями и не трогает её статус', async () => {
    const user = userEvent.setup();
    render(
      <ConceptEntryExpand
        slug="abstraktnyj-trud"
        referenceId={473}
        pageId={9143}
        cuts={[editableCut(), vanishedPartsCut()]}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
    );

    await user.click(screen.getByRole('button', { name: /править границы/ }));
    await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

    await waitFor(() => expect(putCuts).toHaveBeenCalled());
    const [, , body] = putCuts.mock.calls[0];
    expect(body.cuts).toHaveLength(2);
    // Вырезка с исчезнувшими частями не может быть описана границами (нет
    // номера страницы её якорей на клиенте) — уходит ссылкой по id, своим
    // прежним статусом machine, не confirmed: человек её не видел.
    expect(body.cuts).toContainEqual({ id: 93 });
    expect(body.cuts).toContainEqual({
      start_page: 730,
      start_offset: 12,
      end_page: 730,
      end_offset: 28,
      status: 'confirmed',
    });
  });

  // Находка 4 финального разбора: существующие тесты покрывали только
  // случай, когда ОБЕ якорные страницы вырезки пропали (vanishedPartsCut,
  // parts: []). Здесь пропала только одна — start — и bounds.start_page_id
  // не совпадает с parts[0].page_id: смешивать start_page из parts с
  // start_offset из bounds в этом случае значит описать границы на чужой
  // странице.
  it('вырезка с одним из двух несовпадающих якорей уходит по id, а не с перепутанными страницей и смещением', async () => {
    const user = userEvent.setup();
    render(
      <ConceptEntryExpand
        slug="abstraktnyj-trud"
        referenceId={473}
        pageId={9143}
        cuts={[editableCut(), mismatchedStartAnchorCut()]}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
    );

    await user.click(screen.getByRole('button', { name: /править границы/ }));
    await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

    await waitFor(() => expect(putCuts).toHaveBeenCalled());
    const [, , body] = putCuts.mock.calls[0];
    expect(body.cuts).toHaveLength(2);
    expect(body.cuts).toContainEqual({ id: 94 });
    // Ни одно правильное описание границами не смешивает start_page 745
    // (из parts) со start_offset 3 (из bounds пропавшей страницы 9999).
    expect(body.cuts).not.toContainEqual(
      expect.objectContaining({ start_page: 745, start_offset: 3 }),
    );
  });

  // Находка 1 финального разбора: вырезка того же адреса на ДРУГОЙ странице,
  // отвязавшаяся от текста при переякоривании (entry.stale_cuts) — у неё
  // parts всегда пуст, поэтому cutToInput через cuts её никогда не увидит.
  // Без отдельного канала (проп staleCuts) редактор молча стёр бы её первым
  // же своим сохранением, хотя сам её не касался.
  it('устаревшая вырезка другой страницы адреса уходит по id вместе с живой, не меняя статус', async () => {
    const user = userEvent.setup();
    const staleCut: ConceptCut = {
      id: 95,
      bounds: { start_page_id: 9500, start_offset: 0, end_page_id: 9500, end_offset: 10 },
      status: 'stale',
      head_quote: '',
      parts: [],
    };

    render(
      <ConceptEntryExpand
        slug="abstraktnyj-trud"
        referenceId={473}
        pageId={9143}
        cuts={[editableCut()]}
        staleCuts={[staleCut]}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
    );

    await user.click(screen.getByRole('button', { name: /править границы/ }));
    await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

    await waitFor(() => expect(putCuts).toHaveBeenCalled());
    const [, , body] = putCuts.mock.calls[0];
    expect(body.cuts).toHaveLength(2);
    // Устаревшая вырезка уходит голой ссылкой по id — сервер сохранит её
    // статус (stale) как есть, потому что запрос вовсе не несёт его.
    expect(body.cuts).toContainEqual({ id: 95 });
    expect(body.cuts).toContainEqual({
      start_page: 730,
      start_offset: 12,
      end_page: 730,
      end_offset: 28,
      status: 'confirmed',
    });
  });

  // Задача 13a: после успешного сохранения границ разворот обновляет и себя
  // (подсветку — новые границы), и свёрнутую запись потока, чтобы человек не
  // увидел новую подсветку рядом со старым заголовком записи.
  describe('обновление после сохранения (задача 13a)', () => {
    it('после успешного сохранения перезапрашивает и разворот, и запись потока', async () => {
      const user = userEvent.setup();
      const replaceEntry = vi.fn().mockResolvedValue(undefined);

      render(
        <ConceptEntryExpand
          slug="abstraktnyj-trud"
          referenceId={473}
          pageId={9143}
          cuts={[editableCut()]}
          replaceEntry={replaceEntry}
        />,
      );
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
      );

      await user.click(screen.getByRole('button', { name: /править границы/ }));
      expandPage.mockClear();

      await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

      await waitFor(() => expect(replaceEntry).toHaveBeenCalledWith(473));
      await waitFor(() => expect(expandPage).toHaveBeenCalledWith('abstraktnyj-trud', 473, 9143));
    });

    it('на неудачном сохранении не зовёт ни разворот, ни запись потока', async () => {
      const user = userEvent.setup();
      const replaceEntry = vi.fn().mockResolvedValue(undefined);
      putCuts.mockRejectedValueOnce(new Error('сеть'));

      render(
        <ConceptEntryExpand
          slug="abstraktnyj-trud"
          referenceId={473}
          pageId={9143}
          cuts={[editableCut()]}
          replaceEntry={replaceEntry}
        />,
      );
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
      );

      await user.click(screen.getByRole('button', { name: /править границы/ }));
      expandPage.mockClear();

      await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

      await waitFor(() => expect(putCuts).toHaveBeenCalled());
      await waitFor(() =>
        expect(screen.getByText(/Не удалось сохранить границы/)).toBeInTheDocument(),
      );
      expect(replaceEntry).not.toHaveBeenCalled();
      expect(expandPage).not.toHaveBeenCalled();
    });

    it('без обработчика (разворот вне потока) сохранение всё равно обновляет саму себя', async () => {
      const user = userEvent.setup();

      render(
        <ConceptEntryExpand
          slug="abstraktnyj-trud"
          referenceId={473}
          pageId={9143}
          cuts={[editableCut()]}
        />,
      );
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /править границы/ })).toBeInTheDocument(),
      );

      await user.click(screen.getByRole('button', { name: /править границы/ }));
      expandPage.mockClear();

      await user.click(screen.getByRole('button', { name: /сохранить границы/ }));

      await waitFor(() => expect(expandPage).toHaveBeenCalledWith('abstraktnyj-trud', 473, 9143));
    });
  });
});
