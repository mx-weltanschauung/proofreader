import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { chaptersApi, closuresApi, worksApi } from '../../services/api';
import type { ActiveClosure, ClosureEntry } from '../../types';
import { ClosuresAdmin } from './ClosuresAdmin';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const ROWS: ClosureEntry[] = [
  {
    kind: 'work',
    scope: 'full',
    entity_id: 7,
    title: 'Том 7',
    closed: true,
    reason: 'письмо издателя о правах',
    actor_name: 'admin',
    at: '2026-10-23T10:00:00Z',
  },
  {
    kind: 'chapter',
    scope: 'full',
    entity_id: 9,
    title: 'Глава девятая',
    closed: false,
    reason: 'закрыли по ошибке',
    actor_name: 'admin',
    at: '2026-10-22T10:00:00Z',
  },
  {
    kind: 'work',
    scope: 'scans',
    entity_id: 7,
    title: 'Том 7',
    closed: true,
    reason: 'скрыть снимки',
    actor_name: 'admin',
    at: '2026-10-21T10:00:00Z',
  },
];

const ACTIVE: ActiveClosure[] = [
  {
    kind: 'work',
    scope: 'full',
    entity_id: 7,
    title: 'Том 7',
    work_id: null,
    slug: null,
    at: '2026-10-23T10:00:00Z',
    reason: 'письмо издателя',
    actor_name: 'admin',
    inherited: true,
  },
  {
    kind: 'chapter',
    scope: 'scans',
    entity_id: 9,
    title: 'Подглава',
    work_id: 7,
    slug: null,
    at: '2026-10-22T10:00:00Z',
    reason: 'сканы',
    actor_name: 'admin',
    inherited: false,
  },
  {
    kind: 'journal',
    scope: 'full',
    entity_id: 3,
    title: 'Журнал',
    work_id: null,
    slug: 'zhurnal',
    at: null,
    reason: '',
    actor_name: '',
    inherited: false,
  },
];

function renderPage() {
  return render(
    <MemoryRouter>
      <ClosuresAdmin />
    </MemoryRouter>,
  );
}

describe('ClosuresAdmin', () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('сначала показывает действующие собственные ограничения с адресами и наследованием', async () => {
    vi.spyOn(closuresApi, 'active').mockReturnValue(ok(ACTIVE));
    const history = vi.spyOn(closuresApi, 'list');
    renderPage();
    await screen.findByText('письмо издателя');
    expect(screen.getByRole('tab', { name: 'Актуальные' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(history).not.toHaveBeenCalled();
    expect(screen.getByText('23.10.2026')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Том 7' })).toHaveAttribute('href', '/works/7');
    expect(screen.getByRole('link', { name: 'Подглава' })).toHaveAttribute(
      'href',
      '/works/7/chapters/9',
    );
    expect(screen.getByRole('link', { name: 'Журнал' })).toHaveAttribute(
      'href',
      '/journals/zhurnal',
    );
    expect(screen.getByText('После открытия останется ограничен сверху')).toBeInTheDocument();
    expect(
      within(screen.getByRole('link', { name: 'Журнал' }).closest('tr')!).getAllByText('—'),
    ).toHaveLength(3);
    expect(screen.getAllByRole('button', { name: 'Открыть' })).toHaveLength(3);
  });

  it('журнал загружается при выборе и показывает прежнюю историю', async () => {
    vi.spyOn(closuresApi, 'active').mockReturnValue(ok([]));
    vi.spyOn(closuresApi, 'list').mockReturnValue(ok(ROWS));
    renderPage();
    await userEvent.click(screen.getByRole('tab', { name: 'Журнал' }));
    await screen.findByText('письмо издателя о правах');
    expect(screen.getByText('Глава девятая')).toBeInTheDocument();
    expect(screen.getByText('открыто')).toBeInTheDocument();
    expect(screen.getByText('скрыты')).toBeInTheDocument();
    expect(screen.getAllByText('admin')).toHaveLength(3);
  });

  it('открывает том из строки с причиной и перечитывает обе вкладки', async () => {
    const active = vi
      .spyOn(closuresApi, 'active')
      .mockReturnValueOnce(ok([ACTIVE[0]]))
      .mockReturnValue(ok([]));
    const history = vi.spyOn(closuresApi, 'list').mockReturnValue(ok(ROWS));
    const open = vi.spyOn(worksApi, 'setClosed').mockReturnValue(ok({} as never));
    vi.spyOn(window, 'prompt').mockReturnValue('  ошибка закрытия  ');
    renderPage();
    await screen.findByText('письмо издателя');
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith(7, false, 'ошибка закрытия'));
    await waitFor(() => expect(active).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByText('Актуальных закрытий нет.')).toBeInTheDocument());
    await userEvent.click(screen.getByRole('tab', { name: 'Журнал' }));
    await screen.findByText('письмо издателя о правах');
    expect(history).toHaveBeenCalled();
  });

  it('открывает сканы главы по адресу её работы', async () => {
    vi.spyOn(closuresApi, 'active')
      .mockReturnValueOnce(ok([ACTIVE[1]]))
      .mockReturnValue(ok([]));
    const open = vi.spyOn(chaptersApi, 'setScansHidden').mockReturnValue(ok({} as never));
    vi.spyOn(window, 'prompt').mockReturnValue('причина открытия');
    renderPage();
    await screen.findByText('Подглава');
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith(7, 9, false, 'причина открытия'));
  });

  it('отмена причины не отправляет PUT', async () => {
    vi.spyOn(closuresApi, 'active').mockReturnValue(ok([ACTIVE[0]]));
    const open = vi.spyOn(worksApi, 'setClosed');
    vi.spyOn(window, 'prompt').mockReturnValue(null);
    renderPage();
    await screen.findByText('письмо издателя');
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    expect(open).not.toHaveBeenCalled();
  });

  it('ошибка открытия остаётся возле строки', async () => {
    vi.spyOn(closuresApi, 'active').mockReturnValue(ok([ACTIVE[0]]));
    vi.spyOn(worksApi, 'setClosed').mockRejectedValue({
      response: { data: { message: 'Не удалось открыть' } },
    });
    vi.spyOn(window, 'prompt').mockReturnValue('основание');
    renderPage();
    await screen.findByText('письмо издателя');
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    await screen.findByText('Не удалось открыть');
    expect(screen.getByRole('link', { name: 'Том 7' })).toBeInTheDocument();
  });

  it('добавляет следующую страницу действующих ограничений', async () => {
    const first = Array.from({ length: 100 }, (_, i) => ({
      ...ACTIVE[0],
      entity_id: i + 1,
      title: `Том ${i + 1}`,
    }));
    const active = vi
      .spyOn(closuresApi, 'active')
      .mockReturnValueOnce(ok(first))
      .mockReturnValueOnce(ok([ACTIVE[1]]));
    renderPage();
    await screen.findByText('Том 100');
    await userEvent.click(screen.getByRole('button', { name: 'Ещё' }));
    await screen.findByText('Подглава');
    expect(active).toHaveBeenNthCalledWith(2, { limit: 100, offset: 100 });
    expect(screen.getByText('Том 1')).toBeInTheDocument();
  });

  it('показывает текст ошибки сервера для действующего списка', async () => {
    vi.spyOn(closuresApi, 'active').mockRejectedValue({
      response: { status: 403, data: { message: 'Только администратор' } },
    });
    renderPage();
    await waitFor(() => expect(screen.getByText('Только администратор')).toBeInTheDocument());
  });
});
