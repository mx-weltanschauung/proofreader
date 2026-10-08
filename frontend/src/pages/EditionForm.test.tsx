import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useParams } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { editionsApi } from '../services/api';
import type { Edition } from '../types';
import { EditionForm } from './EditionForm';
import { numericId } from '../utils/paths';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

// Заглушка страницы собрания: печатает id из маршрута, чтобы тест мог
// отличить «ушли на страницу созданного/правленого собрания» от «ушли
// вообще куда-то под /editions». Разбирает сегмент через numericId, как и
// настоящий EditionDetail.tsx — адрес теперь может нести слаг (задача 3:
// editionPath откатывается на editions.slug при пустом url_slug), а не
// голый номер.
function EditionDetailStub() {
  const { id } = useParams<{ id: string }>();
  return <div>страница собрания {numericId(id)}</div>;
}

const EDITION: Edition = {
  id: 1,
  title: 'Сочинения, 2-е изд.',
  slug: 'mae-2',
  description: 'Маркс и Энгельс',
  created_at: '',
  updated_at: '',
};

// Таблица маршрутов теста — подмножество App.tsx: /editions отдельным экраном
// в приложении больше нет (перехват-всё уводит на /), поэтому и тест не
// заводит для него заглушку — иначе он проверял бы таблицу маршрутов,
// которой в приложении не существует, и остался бы зелёным даже если бы
// реальная навигация падала в 404.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/editions/new" element={<EditionForm />} />
        <Route path="/editions/:id/edit" element={<EditionForm />} />
        <Route path="/editions/:id" element={<EditionDetailStub />} />
        <Route path="/" element={<div>читальня</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('EditionForm', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('создаёт собрание и уходит на его страницу', async () => {
    const create = vi.spyOn(editionsApi, 'create').mockImplementation(() => ok(EDITION));

    renderAt('/editions/new');

    await userEvent.type(screen.getByLabelText(/Название/), 'Сочинения, 2-е изд.');
    await userEvent.clear(screen.getByLabelText('Слаг *'));
    await userEvent.type(screen.getByLabelText('Слаг *'), 'mae-2');
    await userEvent.click(screen.getByRole('button', { name: 'Создать' }));

    expect(create).toHaveBeenCalledWith({
      title: 'Сочинения, 2-е изд.',
      slug: 'mae-2',
      url_slug: '',
      description: '',
    });
    // Именно созданное собрание (id из ответа сервера), а не /editions
    // вообще — этого экрана в приложении больше нет.
    expect(await screen.findByText(`страница собрания ${EDITION.id}`)).toBeInTheDocument();
  });

  it('подсказывает слаг транслитерацией названия', async () => {
    vi.spyOn(editionsApi, 'create').mockImplementation(() => ok(EDITION));

    renderAt('/editions/new');

    await userEvent.type(screen.getByLabelText(/Название/), 'Сочинения Маркса');

    expect(screen.getByLabelText('Слаг *')).toHaveValue('sochineniya-marksa');
  });

  it('не перебивает слаг, который правили руками', async () => {
    // Подсказка обязана замолчать после ручной правки, иначе набор названия
    // затрёт осознанно выбранный слаг.
    vi.spyOn(editionsApi, 'create').mockImplementation(() => ok(EDITION));

    renderAt('/editions/new');

    await userEvent.type(screen.getByLabelText('Слаг *'), 'mae-2');
    await userEvent.type(screen.getByLabelText(/Название/), 'Сочинения');

    expect(screen.getByLabelText('Слаг *')).toHaveValue('mae-2');
  });

  it('в режиме правки подставляет значения и шлёт PUT', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    const update = vi.spyOn(editionsApi, 'update').mockImplementation(() => ok(EDITION));

    renderAt('/editions/1/edit');

    await waitFor(() => expect(screen.getByLabelText('Слаг *')).toHaveValue('mae-2'));
    await userEvent.clear(screen.getByLabelText(/Описание/));
    await userEvent.type(screen.getByLabelText(/Описание/), 'Второе издание');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(update).toHaveBeenCalledWith(1, {
      title: 'Сочинения, 2-е изд.',
      slug: 'mae-2',
      url_slug: '',
      description: 'Второе издание',
    });
    // Правка возвращает на страницу собрания, а не на /editions — этого
    // экрана в приложении больше нет.
    expect(await screen.findByText(`страница собрания ${EDITION.id}`)).toBeInTheDocument();
  });

  it('в режиме правки не подсказывает слаг при изменении названия', async () => {
    // Слаг участвует в маршрутах (/editions/:id/...): если правка названия
    // затрёт уже сохранённый слаг, ссылки на собрание перестанут открываться.
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));

    renderAt('/editions/1/edit');

    await waitFor(() => expect(screen.getByLabelText('Слаг *')).toHaveValue('mae-2'));
    await userEvent.type(screen.getByLabelText(/Название/), ', доп. том');

    expect(screen.getByLabelText('Слаг *')).toHaveValue('mae-2');
  });

  it('набранный адресный слаг уезжает в url_slug при создании', async () => {
    const create = vi.spyOn(editionsApi, 'create').mockImplementation(() => ok(EDITION));

    renderAt('/editions/new');

    await userEvent.type(screen.getByLabelText(/Название/), 'Сочинения, 2-е изд.');
    await userEvent.clear(screen.getByLabelText('Слаг *'));
    await userEvent.type(screen.getByLabelText('Слаг *'), 'mae-2');
    await userEvent.type(screen.getByLabelText('Слаг для адресов'), 'mae');
    await userEvent.click(screen.getByRole('button', { name: 'Создать' }));

    expect(create).toHaveBeenCalledWith({
      title: 'Сочинения, 2-е изд.',
      slug: 'mae-2',
      url_slug: 'mae',
      description: '',
    });
  });

  it('в режиме правки подставляет пришедший с сервера url_slug и шлёт его обратно', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() =>
      ok({ ...EDITION, url_slug: 'mae-classic' }),
    );
    const update = vi.spyOn(editionsApi, 'update').mockImplementation(() => ok(EDITION));

    renderAt('/editions/1/edit');

    await waitFor(() =>
      expect(screen.getByLabelText('Слаг для адресов')).toHaveValue('mae-classic'),
    );

    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(update).toHaveBeenCalledWith(1, {
      title: 'Сочинения, 2-е изд.',
      slug: 'mae-2',
      url_slug: 'mae-classic',
      description: 'Маркс и Энгельс',
    });
  });

  // Регрессия: isEditMode раньше выводился из сырого idParam, а ветка
  // отправки — из разобранного id. На битом сегменте (/editions/xyz/edit)
  // это расходилось: форма рисовалась как «Новое собрание» (и к тому же
  // вечно висела на «Загрузка…», потому что загрузчик выходил раньше
  // setIsLoading(false)), а отправка тихо создавала бы собрание вместо
  // честного отказа.
  it('на битом id адреса показывает отказ вместо формы и ничего не создаёт', async () => {
    const create = vi.spyOn(editionsApi, 'create');
    const update = vi.spyOn(editionsApi, 'update');
    const get = vi.spyOn(editionsApi, 'get');

    renderAt('/editions/xyz/edit');

    expect(await screen.findByText('Собрание не найдено')).toBeInTheDocument();
    expect(screen.queryByText('Загрузка…')).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Название/)).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
    expect(update).not.toHaveBeenCalled();
  });

  // Раньше и «назад», и «Отмена» на обеих формах вели на /editions — экран,
  // которого в приложении больше нет, и оба работали только благодаря
  // перехвату-всё, молча уводившему на /.
  it('у новой карточки «назад» и «Отмена» ведут на главную', async () => {
    renderAt('/editions/new');

    expect(screen.getByRole('link', { name: '← В читальню' })).toHaveAttribute('href', '/');

    await userEvent.click(screen.getByRole('button', { name: 'Отмена' }));

    expect(await screen.findByText('читальня')).toBeInTheDocument();
  });

  it('у правки «назад» и «Отмена» ведут на страницу собрания', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));

    renderAt('/editions/1/edit');

    await waitFor(() =>
      expect(screen.getByRole('link', { name: '← К собранию' })).toHaveAttribute(
        'href',
        '/editions/1',
      ),
    );

    await userEvent.click(screen.getByRole('button', { name: 'Отмена' }));

    expect(await screen.findByText(`страница собрания ${EDITION.id}`)).toBeInTheDocument();
  });
});
