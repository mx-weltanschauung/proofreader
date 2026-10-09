import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import toast from 'react-hot-toast';
import { chaptersApi, personsApi, worksApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Chapter, Work } from '../types';
import { ChapterForm } from './ChapterForm';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const WORK = { id: 5, title: 'Том 5', page_offset: 0 } as Work;

const CHAPTER = {
  id: 42,
  work_id: 5,
  title: 'Глава вторая',
  type: 'chapter',
  order_number: 2,
  start_page: 10,
  end_page: 20,
} as Chapter;

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/works/:workId/chapters/new" element={<ChapterForm />} />
        <Route path="/works/:workId/chapters/:chapterId/edit" element={<ChapterForm />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('ChapterForm', () => {
  beforeEach(() => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('в режиме правки существующей главы шлёт PUT', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    const update = vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));

    renderAt('/works/5/chapters/42/edit');

    await waitFor(() => expect(screen.getByLabelText(/Заглавие/)).toHaveValue('Глава вторая'));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(update).toHaveBeenCalledWith(5, 42, expect.objectContaining({ title: 'Глава вторая' }));
  });

  it('в режиме создания новой главы шлёт POST', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    const create = vi.spyOn(chaptersApi, 'create').mockImplementation(() => ok(CHAPTER));

    renderAt('/works/5/chapters/new');

    await waitFor(() => expect(screen.getByLabelText(/Заглавие/)).toBeInTheDocument());
    await userEvent.type(screen.getByLabelText(/Заглавие/), 'Новая глава');
    await userEvent.click(screen.getByRole('button', { name: 'Создать главу' }));

    expect(create).toHaveBeenCalledWith(5, expect.objectContaining({ title: 'Новая глава' }));
  });

  const JOURNAL_CHAPTER = {
    ...CHAPTER,
    article_kind: 'статья',
    credits: [{ position: 1, role: 'author', printed: 'В. Повняков' }],
  } as Chapter;

  it('у главы номера журнала правится вид статьи и подпись', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, role: 'journal_issue' } as Work),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(JOURNAL_CHAPTER));
    const update = vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    const put = vi.spyOn(chaptersApi, 'putCredits').mockImplementation(() => ok([]));
    renderAt('/works/5/chapters/42/edit');
    fireEvent.change(await screen.findByLabelText('Вид статьи'), { target: { value: 'рецензия' } });
    fireEvent.change(screen.getByLabelText('Подпись 1'), { target: { value: 'В. Позняков' } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить подпись' }));
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(put).toHaveBeenCalled());
    expect(update.mock.calls[0][2]).toMatchObject({ article_kind: 'рецензия' });
    expect(update.mock.calls[0][2]).not.toHaveProperty('credits');
    expect(put.mock.calls[0][2]).toEqual([
      { role: 'author', printed: 'В. Позняков', person_id: null },
    ]);
  });

  // Финальная рецензия: человек подписи был голым числовым полем, а номер
  // человека не показан ни на одном экране. Теперь — поиск по фамилии.
  it('человек подписи выбирается поиском по фамилии', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, role: 'journal_issue' } as Work),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(JOURNAL_CHAPTER));
    vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    const put = vi.spyOn(chaptersApi, 'putCredits').mockImplementation(() => ok([]));
    const search = vi
      .spyOn(personsApi, 'search')
      .mockImplementation(() =>
        ok([{ id: 9, name: 'В. Позняков', sort_key: 'позняков в', slug: 'v-poznyakov' }]),
      );
    renderAt('/works/5/chapters/42/edit');
    fireEvent.change(await screen.findByLabelText('Найти человека 1'), {
      target: { value: 'Позн' },
    });
    await waitFor(() => expect(search).toHaveBeenCalledWith('Позн'));
    fireEvent.click(await screen.findByRole('button', { name: 'В. Позняков' }));
    expect(screen.getByRole('link', { name: 'В. Позняков' })).toHaveAttribute(
      'href',
      '/authors/v-poznyakov',
    );
    expect(screen.queryByLabelText('Найти человека 1')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(put).toHaveBeenCalled());
    expect(put.mock.calls[0][2]).toEqual([
      { role: 'author', printed: 'В. Повняков', person_id: 9 },
    ]);
  });

  it('привязанного человека снимает «Без человека»', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, role: 'journal_issue' } as Work),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() =>
      ok({
        ...JOURNAL_CHAPTER,
        credits: [
          {
            position: 1,
            role: 'author',
            printed: 'Гр. Баммель',
            person_id: 4,
            person_slug: 'gr-bammel',
          },
        ],
      } as Chapter),
    );
    vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    const put = vi.spyOn(chaptersApi, 'putCredits').mockImplementation(() => ok([]));
    renderAt('/works/5/chapters/42/edit');
    expect(await screen.findByRole('link', { name: 'gr-bammel' })).toHaveAttribute(
      'href',
      '/authors/gr-bammel',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Без человека 1' }));
    expect(screen.getByLabelText('Найти человека 1')).toHaveValue('');
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
    expect(put.mock.calls[0][2]).toEqual([
      { role: 'author', printed: 'Гр. Баммель', person_id: null },
    ]);
  });

  it('привязанный человек без правки уезжает прежним', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, role: 'journal_issue' } as Work),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() =>
      ok({
        ...JOURNAL_CHAPTER,
        credits: [
          {
            position: 1,
            role: 'author',
            printed: 'Гр. Баммель',
            person_id: 4,
            person_slug: 'gr-bammel',
          },
        ],
      } as Chapter),
    );
    vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    const put = vi.spyOn(chaptersApi, 'putCredits').mockImplementation(() => ok([]));
    renderAt('/works/5/chapters/42/edit');
    expect(await screen.findByRole('link', { name: 'gr-bammel' })).toHaveAttribute(
      'href',
      '/authors/gr-bammel',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
    expect(put.mock.calls[0][2]).toEqual([
      { role: 'author', printed: 'Гр. Баммель', person_id: 4 },
    ]);
  });

  it('«не статья (рубрика)» шлёт пустой вид — сервер иначе оставил бы прежний', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, role: 'journal_issue' } as Work),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(JOURNAL_CHAPTER));
    const update = vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(chaptersApi, 'putCredits').mockImplementation(() => ok([]));
    renderAt('/works/5/chapters/42/edit');
    fireEvent.change(await screen.findByLabelText('Вид статьи'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][2]).toMatchObject({ article_kind: '' });
  });

  it('провал подписи при создании: ошибка, без тоста успеха, повтор не создаёт вторую главу', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, role: 'journal_issue' } as Work),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    const create = vi.spyOn(chaptersApi, 'create').mockImplementation(() => ok(CHAPTER));
    const update = vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    const put = vi
      .spyOn(chaptersApi, 'putCredits')
      .mockImplementationOnce(() => Promise.reject(new Error('boom')))
      .mockImplementation(() => ok([]));
    const success = vi.spyOn(toast, 'success');
    renderAt('/works/5/chapters/new');
    fireEvent.change(await screen.findByLabelText(/Заглавие/), { target: { value: 'Статья' } });
    fireEvent.click(screen.getByRole('button', { name: 'Создать главу' }));
    expect(await screen.findByText(/Глава сохранена, подпись не сохранилась/)).toBeInTheDocument();
    expect(success).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Создать главу' }));
    await waitFor(() => expect(put).toHaveBeenCalledTimes(2));
    expect(create).toHaveBeenCalledTimes(1);
    expect(update).toHaveBeenCalledWith(5, 42, expect.anything());
    expect(put.mock.calls[1][1]).toBe(42);
  });

  it('у главы тома полей статьи нет и article_kind не уходит', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok({ ...WORK, role: 'volume' } as Work));
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    const update = vi.spyOn(chaptersApi, 'update').mockImplementation(() => ok(CHAPTER));
    const put = vi.spyOn(chaptersApi, 'putCredits');
    renderAt('/works/5/chapters/42/edit');
    await screen.findByLabelText(/Заглавие/);
    expect(screen.queryByLabelText('Вид статьи')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][2]).not.toHaveProperty('article_kind');
    expect(put).not.toHaveBeenCalled();
  });

  // Регрессия: isEditMode раньше выводился из сырого chapterParam, а ветка
  // отправки — из разобранного chapterId. На битом сегменте
  // (/works/5/chapters/xyz/edit) это расходилось: форма рисовалась как
  // «Новая глава» без предупреждения, а отправка тихо создавала бы новую
  // главу вместо честного отказа с сообщением «Глава не найдена».
  it('на битом id главы в адресе показывает отказ вместо формы и ничего не создаёт', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([]));
    const get = vi.spyOn(chaptersApi, 'get');
    const create = vi.spyOn(chaptersApi, 'create');
    const update = vi.spyOn(chaptersApi, 'update');

    renderAt('/works/5/chapters/xyz/edit');

    expect(await screen.findByText('Глава не найдена')).toBeInTheDocument();
    expect(screen.queryByText('Новая глава')).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Заглавие/)).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
    expect(update).not.toHaveBeenCalled();
  });

  // Регрессия уровнем выше: тот же силуэт на сегменте работы. Эффект выходил
  // рано на workId === null, не сбросив isLoading, — компонент вечно висел на
  // «Загрузка…» вместо уже существующего экрана «Работа не найдена».
  it('на битом id работы в адресе показывает отказ, а не вечную загрузку, и ничего не грузит', async () => {
    const workGet = vi.spyOn(worksApi, 'get');
    const list = vi.spyOn(chaptersApi, 'list');
    const create = vi.spyOn(chaptersApi, 'create');
    const update = vi.spyOn(chaptersApi, 'update');

    renderAt('/works/xyz/chapters/new');

    expect(await screen.findByText('Работа не найдена')).toBeInTheDocument();
    expect(screen.queryByText('Загрузка…')).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Заглавие/)).not.toBeInTheDocument();
    expect(workGet).not.toHaveBeenCalled();
    expect(list).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
    expect(update).not.toHaveBeenCalled();
  });
});
