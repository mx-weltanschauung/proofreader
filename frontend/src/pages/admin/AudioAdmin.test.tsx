import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { AxiosError, type AxiosResponse } from 'axios';
import { audioApi } from '../../services/api';
import type { AudioQueue, AudioQueueItem } from '../../types';
import { AudioAdmin } from './AudioAdmin';

vi.mock('../../services/api', () => ({
  audioApi: { queue: vi.fn(), cancel: vi.fn(), retry: vi.fn(), requeueStale: vi.fn() },
}));
vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function item(
  id: number,
  status: AudioQueueItem['status'],
  chapter: string | null,
  chapterId = 223,
): AudioQueueItem {
  return {
    id,
    work_id: 4,
    work_title: 'Том 4',
    chapter_id: chapter ? chapterId : null,
    chapter_title: chapter ?? '',
    status,
    error: status === 'ошибка' ? 'синтез упал: процесс убит (память?)' : '',
    status_counts: {},
    requested_by: 'editor@x',
    requested_at: '2026-10-01T10:00:00Z',
    claimed_at: null,
    finished_at: null,
  };
}

const QUEUE: AudioQueue = {
  items: [
    item(1, 'в_очереди', 'Глава'),
    item(2, 'синтезируется', null),
    item(3, 'ошибка', 'Другая глава', 224),
  ],
  stale: [{ work_id: 4, work_title: 'Том 4', stale: 3 }],
};

function renderAdmin() {
  return render(
    <MemoryRouter>
      <AudioAdmin />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(audioApi.queue).mockReturnValue(ok(QUEUE));
});

describe('AudioAdmin', () => {
  it('очередь, ошибки текстом целиком, устаревшее', async () => {
    renderAdmin();
    const queue = await screen.findByRole('region', { name: 'Очередь' });
    expect(within(queue).getByText(/Том 4 — Глава/)).toBeInTheDocument();
    expect(within(queue).getByText(/Том 4 — весь том/)).toBeInTheDocument();
    // «снять» — только у ждущей: синтезируемую сервер не снимает.
    expect(within(queue).getAllByRole('button', { name: 'снять' })).toHaveLength(1);
    const errors = screen.getByRole('region', { name: 'Ошибки' });
    expect(within(errors).getByText('синтез упал: процесс убит (память?)')).toBeInTheDocument();
    const stale = screen.getByRole('region', { name: 'Устаревшее' });
    expect(within(stale).getByText(/Том 4/)).toHaveTextContent('3');
  });

  it('снять, повторить, поставить заново — и перечитать', async () => {
    vi.mocked(audioApi.cancel).mockReturnValue(ok(undefined));
    vi.mocked(audioApi.retry).mockReturnValue(ok(item(3, 'в_очереди', 'Другая глава', 224)));
    vi.mocked(audioApi.requeueStale).mockReturnValue(ok({ queued: 1, items: [] }));
    renderAdmin();
    const queue = await screen.findByRole('region', { name: 'Очередь' });
    await userEvent.click(within(queue).getByRole('button', { name: 'снять' }));
    expect(audioApi.cancel).toHaveBeenCalledWith(1);
    await userEvent.click(screen.getByRole('button', { name: 'повторить' }));
    expect(audioApi.retry).toHaveBeenCalledWith(3);
    await userEvent.click(screen.getByRole('button', { name: 'поставить заново' }));
    expect(audioApi.requeueStale).toHaveBeenCalledWith(4);
    await waitFor(() => expect(vi.mocked(audioApi.queue).mock.calls.length).toBe(4));
  });

  // Рецензия, п. 2: упавшая заявка висит бессрочно, а повтор постоянного
  // отказа падает снова — её надо уметь снять (сервер снимает и «ошибку»).
  it('упавшую заявку можно снять', async () => {
    vi.mocked(audioApi.cancel).mockReturnValue(ok(undefined));
    renderAdmin();
    const errors = await screen.findByRole('region', { name: 'Ошибки' });
    await userEvent.click(within(errors).getByRole('button', { name: 'снять' }));
    expect(audioApi.cancel).toHaveBeenCalledWith(3);
    await waitFor(() => expect(vi.mocked(audioApi.queue).mock.calls.length).toBe(2));
  });

  // Тикет 07, п. 7: у главы уже есть открытая заявка — повтор упавшей упрётся
  // в уникальный индекс, и сервер ответит невнятным «обновите список».
  it('у главы уже есть открытая заявка — вместо «повторить» объяснение', async () => {
    vi.mocked(audioApi.queue).mockReturnValue(
      ok({
        items: [
          item(1, 'в_очереди', 'Глава'),
          item(3, 'ошибка', 'Глава'),
          item(5, 'синтезируется', null),
          item(6, 'ошибка', null),
          item(7, 'ошибка', 'Третья', 225),
        ],
        stale: [],
      }),
    );
    renderAdmin();
    const errors = await screen.findByRole('region', { name: 'Ошибки' });
    const rows = within(errors).getAllByRole('listitem');
    for (const row of rows.slice(0, 2)) {
      expect(within(row).queryByRole('button', { name: 'повторить' })).toBeNull();
      expect(within(row).getByText(/уже стоит открытая заявка/)).toBeInTheDocument();
      expect(within(row).getByRole('button', { name: 'снять' })).toBeInTheDocument();
    }
    // Другая глава того же тома — повтор законен.
    expect(within(rows[2]).getByRole('button', { name: 'повторить' })).toBeInTheDocument();
    expect(within(rows[2]).queryByText(/уже стоит открытая заявка/)).toBeNull();
  });

  it('открытая заявка того же номера главы в другом томе — повтор законен', async () => {
    vi.mocked(audioApi.queue).mockReturnValue(
      ok({
        items: [{ ...item(1, 'в_очереди', 'Глава'), work_id: 5 }, item(3, 'ошибка', 'Глава')],
        stale: [],
      }),
    );
    renderAdmin();
    const errors = await screen.findByRole('region', { name: 'Ошибки' });
    expect(within(errors).getByRole('button', { name: 'повторить' })).toBeInTheDocument();
  });

  // Список устарел: открытую заявку поставили из главы уже после загрузки
  // экрана. 409 перечитывает список, и объяснение появляется само.
  it('409 на повторе — список перечитан, причина видна', async () => {
    vi.mocked(audioApi.queue)
      .mockReturnValueOnce(ok({ items: [item(3, 'ошибка', 'Глава')], stale: [] }))
      .mockReturnValue(
        ok({ items: [item(1, 'в_очереди', 'Глава'), item(3, 'ошибка', 'Глава')], stale: [] }),
      );
    const conflict = new AxiosError('Request failed with status code 409', 'ERR_BAD_REQUEST');
    conflict.response = {
      status: 409,
      data: { message: 'Заявка в другом состоянии — обновите список' },
    } as AxiosResponse;
    vi.mocked(audioApi.retry).mockImplementation(() => Promise.reject(conflict));
    renderAdmin();
    await userEvent.click(await screen.findByRole('button', { name: 'повторить' }));
    expect(await screen.findByText(/уже стоит открытая заявка/)).toBeInTheDocument();
    expect(audioApi.queue).toHaveBeenCalledTimes(2);
  });

  it('пусто — так и говорит', async () => {
    vi.mocked(audioApi.queue).mockReturnValue(ok({ items: [], stale: [] }));
    renderAdmin();
    expect(await screen.findByText('Очередь пуста')).toBeInTheDocument();
  });
});
