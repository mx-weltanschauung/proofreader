import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { collectionsApi } from '../services/api';
import type { Collection } from '../types';
import { CollectionRead } from './CollectionRead';
import { SITE_NAME } from '../hooks/useDocumentTitle';
import { storageKey } from '../hooks/useReadingProgress';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';

vi.mock('katex/dist/contrib/auto-render', () => ({ default: () => {} }));
vi.mock('../services/api', () => ({
  collectionsApi: { get: vi.fn(), itemPages: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const COLLECTION: Collection = {
  id: 1,
  title: 'Материализм',
  slug: 'materializm',
  description: '',
  created_at: '',
  updated_at: '',
  author_nickname: '',
  items: [
    {
      id: 1,
      kind: 'chapter',
      order_number: 1,
      title: 'Тезисы о Фейербахе',
      work_author: 'К. Маркс',
      author_override: '',
      author: 'К. Маркс',
      broken: false,
      source: { work_id: 41, work_title: 'Том 3', volume_number: 3, page_start: 1, page_end: 4 },
    },
    {
      id: 2,
      kind: 'chapter',
      order_number: 2,
      title: 'Людвиг Фейербах',
      work_author: 'Ф. Энгельс',
      author_override: '',
      author: 'Ф. Энгельс',
      broken: false,
      source: {
        work_id: 42,
        work_title: 'Том 21',
        volume_number: 21,
        page_start: 269,
        page_end: 317,
      },
    },
  ],
};

const PAGES = {
  pages: [
    {
      page_number: 1,
      html: '<p>Философы лишь различным образом объясняли мир…</p>',
      blank: false,
    },
  ],
  footnotes_html: '',
};

function renderRead(itemId = '1') {
  return render(
    <MemoryRouter initialEntries={[`/collections/materializm/read/${itemId}`]}>
      <ReadingPreferencesProvider>
        <Routes>
          <Route path="/collections/:slug/read/:itemId" element={<CollectionRead />} />
        </Routes>
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

describe('CollectionRead', () => {
  beforeEach(() => {
    vi.mocked(collectionsApi.get).mockReturnValue(ok(COLLECTION));
    vi.mocked(collectionsApi.itemPages).mockReturnValue(ok(PAGES));
  });

  it('показывает текст элемента и его заголовок', async () => {
    renderRead();

    expect(await screen.findByText('Тезисы о Фейербахе')).toBeTruthy();
    expect(screen.getByText(/Философы лишь различным образом/)).toBeTruthy();
  });

  // F4 итогового ревью: заголовок вкладки не был проложен вовсе — читатель
  // видел «Читальня» и на списке, и на чтении пункта, тогда как сервер
  // (internal/seo, маршрут collections/{slug}/read/{id}) отдаёт краулеру
  // Doc самой подборки. Заголовок обязан браться у подборки, а не у пункта:
  // «Тезисы о Фейербахе» — заголовок пункта, а не то, что печатает сервер.
  it('ставит заголовок вкладки по подборке, а не по читаемому пункту', async () => {
    document.title = SITE_NAME;
    renderRead();

    await screen.findByText('Тезисы о Фейербахе');
    // Заголовок ставится эффектом, а проверка идёт сразу за ожиданием
    // разметки — под нагрузкой полного прогона эффект успевает не всегда.
    // Синхронная проверка здесь давала нестабильный провал примерно раз
    // на два прогона: ждём сам заголовок, а не разметку рядом с ним.
    await waitFor(() => {
      expect(document.title).toBe(`Материализм — подборка — ${SITE_NAME}`);
    });
  });

  it('ведёт «дальше» к следующему элементу подборки, а не к соседней главе тома', async () => {
    const { container } = renderRead();

    await screen.findByText('Тезисы о Фейербахе');
    expect(container.querySelector('a[href="/collections/materializm/read/2"]')).not.toBeNull();
  });

  // Элемент подборки не несёт ни автора, ни названия работы, ни издания
  // (entry.source их не хранит) — собрать подпись цитаты не из чего, поэтому
  // экран сознательно не передаёт citation в ReadingSurface (см. CollectionRead.tsx).
  it('не показывает кнопку «Цитировать»/«Ссылка» — подпись собрать не из чего', async () => {
    renderRead();

    await screen.findByText('Тезисы о Фейербахе');
    expect(screen.queryByRole('button', { name: /Цитировать|Ссылка/ })).not.toBeInTheDocument();
  });

  it('объясняет 410 по-человечески, а не общей ошибкой', async () => {
    vi.mocked(collectionsApi.itemPages).mockRejectedValue({
      response: { status: 410 },
      isAxiosError: true,
    });

    renderRead();

    expect(await screen.findByText(/источник удалён/i)).toBeTruthy();
  });
});

// --- Где открывается следующая запись подборки ---------------------------
// То же, что и у соседней главы тома: CollectionRead не размонтируется между
// записями (Route тот же, меняется только :itemId), прокрутку окна не
// сбрасывает никто, и следующая запись открывалась на смещении предыдущей —
// у записи покороче браузер прижимал его к самому низу.

describe('CollectionRead: где открывается следующая запись', () => {
  const PAGES_TWO = {
    pages: [{ page_number: 269, html: '<p>Людвиг Фейербах и конец…</p>', blank: false }],
    footnotes_html: '',
  };

  function deferred() {
    let resolve!: () => void;
    const promise = new Promise<void>((r) => {
      resolve = r;
    });
    return { promise, resolve };
  }

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    window.scrollTo = vi.fn();
    // Читатель дочитал первую запись до конца: jsdom вёрстки не считает,
    // поэтому смещение задаётся руками.
    Object.defineProperty(document.documentElement, 'scrollTop', {
      value: 5000,
      writable: true,
      configurable: true,
    });
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    }) as typeof window.requestAnimationFrame;
    window.cancelAnimationFrame = vi.fn();
    vi.mocked(collectionsApi.get).mockReturnValue(ok(COLLECTION));
  });

  // Ответ по второй записи держится на затворе: окно «адрес уже от второй,
  // текст ещё от первой» иначе живёт один тик.
  function gateSecondItem() {
    const gate = deferred();
    vi.mocked(collectionsApi.itemPages).mockImplementation(async (_slug: string, id: number) => {
      if (id === 2) await gate.promise;
      return ok(id === 2 ? PAGES_TWO : PAGES);
    });
    return gate;
  }

  it('открывает следующую запись сверху, а не там, где брошена прежняя', async () => {
    const gate = gateSecondItem();
    renderRead();
    await screen.findByText('Тезисы о Фейербахе');
    vi.mocked(window.scrollTo).mockClear();

    await userEvent.click(screen.getAllByRole('link', { name: /Людвиг Фейербах/ })[0]);
    expect(window.scrollTo).not.toHaveBeenCalled();

    await act(async () => {
      gate.resolve();
      await gate.promise;
    });
    await screen.findByText(/Людвиг Фейербах и конец/);

    expect(window.scrollTo).toHaveBeenCalledWith(0, 0);
  });

  // Позиция пишется по ключу записи. Смещение прежней записи под ключом новой
  // переживёт перезагрузку: в следующий раз запись «восстановится» на конце
  // соседней.
  it('не записывает смещение прежней записи под ключ новой', async () => {
    gateSecondItem();
    renderRead();
    await screen.findByText('Тезисы о Фейербахе');

    await userEvent.click(screen.getAllByRole('link', { name: /Людвиг Фейербах/ })[0]);

    expect(localStorage.getItem(storageKey(42, 2))).toBeNull();
  });
});
