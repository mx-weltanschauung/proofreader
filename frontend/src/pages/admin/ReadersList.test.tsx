import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { readersApi, collectionsApi } from '../../services/api';
import type { AdminReader, AdminReaderCollection } from '../../types';
import { ReadersList } from './ReadersList';

vi.mock('../../services/api', () => ({
  readersApi: {
    list: vi.fn(),
    collections: vi.fn(),
  },
  collectionsApi: {
    remove: vi.fn(),
  },
}));

vi.mock('react-hot-toast', () => ({
  default: { success: vi.fn(), error: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const READERS: AdminReader[] = [
  { id: 7, nickname: 'Чтец', created_at: '2026-09-17T10:00:00Z', collections_count: 2 },
  { id: 8, nickname: 'книгочей', created_at: '2026-09-12T10:00:00Z', collections_count: 0 },
];

const CHTEC_COLLECTIONS: AdminReaderCollection[] = [
  {
    id: 1,
    title: 'Ранний Маркс',
    slug: 'ranniy-marks',
    description: '',
    author_nickname: 'Чтец',
    published_at: '2026-09-17T12:00:00Z',
    created_at: '2026-09-17T11:00:00Z',
    updated_at: '2026-09-17T12:00:00Z',
    was_published: true,
  },
  {
    id: 2,
    title: 'Черновик',
    slug: 'chernovik',
    description: '',
    author_nickname: 'Чтец',
    created_at: '2026-09-17T11:30:00Z',
    updated_at: '2026-09-17T11:30:00Z',
    was_published: false,
  },
];

describe('читатели глазами администратора', () => {
  beforeEach(() => {
    vi.mocked(readersApi.list).mockReset();
    vi.mocked(readersApi.collections).mockReset();
    vi.mocked(collectionsApi.remove).mockReset();
    vi.mocked(readersApi.list).mockReturnValue(ok(READERS));
    vi.mocked(readersApi.collections).mockReturnValue(ok(CHTEC_COLLECTIONS));
  });

  function renderList() {
    return render(
      <MemoryRouter>
        <ReadersList />
      </MemoryRouter>,
    );
  }

  it('показывает читателей и число их подборок', async () => {
    renderList();

    expect(await screen.findByText('Чтец')).toBeInTheDocument();
    expect(screen.getByText('книгочей')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('ищет читателя по нику из жалобы', async () => {
    renderList();
    await screen.findByText('Чтец');

    await userEvent.type(screen.getByRole('searchbox', { name: /ник/i }), 'чтец');
    await userEvent.click(screen.getByRole('button', { name: 'Найти' }));

    await waitFor(() => expect(readersApi.list).toHaveBeenLastCalledWith('чтец'));
  });

  it('раскрывает подборки читателя, включая черновик', async () => {
    renderList();
    await screen.findByText('Чтец');

    await userEvent.click(screen.getByRole('button', { name: /Чтец/ }));

    expect(await screen.findByRole('link', { name: 'Ранний Маркс' })).toHaveAttribute(
      'href',
      '/collections/%D0%A7%D1%82%D0%B5%D1%86/ranniy-marks',
    );
    expect(screen.getByText('Черновик')).toBeInTheDocument();
    expect(screen.getByText('черновик')).toBeInTheDocument();
    // Черновик и снятая с публикации видны только владельцу (404 и 410
    // постороннему, включая администратора), поэтому ссылкой не рисуются:
    // она вела бы в отказ. Снять их при этом можно.
    expect(screen.queryByRole('link', { name: 'Черновик' })).toBeNull();
    expect(readersApi.collections).toHaveBeenCalledWith('Чтец');
  });

  it('снимает подборку только после подтверждения', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    vi.mocked(collectionsApi.remove).mockReturnValue(ok(undefined));
    renderList();
    await screen.findByText('Чтец');
    await userEvent.click(screen.getByRole('button', { name: /Чтец/ }));
    await screen.findByRole('link', { name: 'Ранний Маркс' });

    await userEvent.click(screen.getAllByRole('button', { name: 'Снять' })[0]);
    expect(collectionsApi.remove).not.toHaveBeenCalled();

    confirmSpy.mockReturnValue(true);
    await userEvent.click(screen.getAllByRole('button', { name: 'Снять' })[0]);
    await waitFor(() => expect(collectionsApi.remove).toHaveBeenCalledWith('ranniy-marks', 'Чтец'));

    confirmSpy.mockRestore();
  });

  // Снятая с публикации и черновик — разные вещи для разбора жалобы: снятую
  // читатели видели, черновика не видел никто, кроме автора. Признак считает
  // сервер (was_published), потому что отметка публикации наружу не уезжает.
  it('отличает снятую с публикации от черновика', async () => {
    vi.mocked(readersApi.collections).mockReturnValue(
      ok([
        {
          id: 3,
          title: 'Снятая',
          slug: 'snyataya',
          description: '',
          author_nickname: 'Чтец',
          created_at: '2026-09-17T11:00:00Z',
          updated_at: '2026-09-17T12:00:00Z',
          was_published: true,
        },
      ]),
    );
    renderList();
    await screen.findByText('Чтец');

    await userEvent.click(screen.getByRole('button', { name: /Чтец/ }));

    expect(await screen.findByText('снята с публикации')).toBeInTheDocument();
    expect(screen.queryByText('черновик')).toBeNull();
  });

  // Ответы приходят не в том порядке, в каком их спросили: два быстрых щелчка
  // подряд — и подборки первого читателя оказались бы под раскрытой строкой
  // второго, ровно в том разборе, ради которого экран и заведён.
  it('не показывает подборки одного читателя под строкой другого', async () => {
    let releaseFirst: () => void = () => {};
    vi.mocked(readersApi.collections).mockImplementation((nickname: string) => {
      if (nickname === 'Чтец') {
        return new Promise((resolve) => {
          releaseFirst = () => resolve({ data: CHTEC_COLLECTIONS } as never);
        });
      }
      return ok([
        {
          id: 9,
          title: 'Книгочеева',
          slug: 'knigocheeva',
          description: '',
          author_nickname: 'книгочей',
          published_at: '2026-09-12T12:00:00Z',
          created_at: '2026-09-12T11:00:00Z',
          updated_at: '2026-09-12T12:00:00Z',
          was_published: true,
        },
      ]);
    });

    renderList();
    await screen.findByText('Чтец');

    await userEvent.click(screen.getByRole('button', { name: /Чтец/ }));
    await userEvent.click(screen.getByRole('button', { name: /книгочей/ }));
    await screen.findByText('Книгочеева');

    releaseFirst();
    await waitFor(() => expect(screen.getByText('Книгочеева')).toBeInTheDocument());
    expect(screen.queryByText('Ранний Маркс')).toBeNull();
  });

  // Учётной записи нет, а подборки под этим именем остались: владельца удалили,
  // owner_id обнулился, подпись живёт снимком ника. Это и есть случай, ради
  // которого ключом взят ник, — и дойти до него администратор должен с экрана,
  // а не голым запросом к API.
  it('находит подборки имени, у которого нет учётной записи', async () => {
    vi.mocked(readersApi.list).mockReturnValue(ok([]));
    vi.mocked(readersApi.collections).mockReturnValue(ok(CHTEC_COLLECTIONS));
    renderList();
    await screen.findByText('Читателей пока нет');

    await userEvent.type(screen.getByRole('searchbox', { name: /ник/i }), 'Чтец');
    await userEvent.click(screen.getByRole('button', { name: 'Найти' }));

    expect(await screen.findByRole('link', { name: 'Ранний Маркс' })).toBeInTheDocument();
    expect(screen.getByText(/учётной записи.*нет/i)).toBeInTheDocument();
    expect(readersApi.collections).toHaveBeenCalledWith('Чтец');
  });

  // Список обрезан потолком, и молчать об этом нельзя: администратор принял бы
  // обрезанную выдачу за полную и решил, что искомого читателя нет.
  it('говорит, что список обрезан потолком', async () => {
    vi.mocked(readersApi.list).mockReturnValue(
      ok(
        Array.from({ length: 200 }, (_, i) => ({
          id: i + 1,
          nickname: `читатель${i + 1}`,
          created_at: '2026-09-17T10:00:00Z',
          collections_count: 0,
        })),
      ),
    );
    renderList();

    expect(await screen.findByText(/показаны первые 200/i)).toBeInTheDocument();
  });

  // Сторож решения владельца: учётной записью читателя администратор не
  // распоряжается. Сброса пароля читальня не умеет вовсе, роль читателя не
  // назначается и не снимается, а удаление учётки не прекращает выданный
  // токен — кнопка обещала бы то, чего нет.
  it('не предлагает ни сброса пароля, ни смены роли, ни удаления читателя', async () => {
    renderList();
    await screen.findByText('Чтец');

    expect(screen.queryByRole('button', { name: /Сбросить пароль/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Удалить/ })).toBeNull();
    expect(screen.queryByRole('combobox')).toBeNull();
  });
});
