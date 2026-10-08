import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { chaptersApi, worksApi } from '../services/api';
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
