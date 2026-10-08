import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { pagesApi, worksApi, suggestionsApi } from '../services/api';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
// useAuth — это и есть zustand-стор (create<AuthState>()), поэтому у него
// есть setState: статус входа в тестах подменяется прямо на нём, без vi.mock.
import { useAuth } from '../hooks/useAuth';
import { pageSha256 } from '../utils/pageSha256';
import { PageSuggest } from './PageSuggest';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const PAGE = {
  id: 42,
  work_id: 7,
  page_number: 3,
  preview_path: 'works/7/pages/page_3.png',
  content_markdown: 'Текст полосы до правки',
  status: 'вычитано_машиной',
  created_at: '',
  updated_at: '',
};

const READER_USER = { id: 1, email: '', nickname: 'читатель', role: 'reader' as const };

function renderSuggest() {
  vi.spyOn(pagesApi, 'getByNumber').mockReturnValue(ok(PAGE) as never);
  vi.spyOn(worksApi, 'get').mockReturnValue(ok({ id: 7, title: 'Том 8', page_offset: 0 }) as never);
  // MarkdownEditor зовёт useReadingPrefs() — в приложении маршрут уже стоит
  // внутри ReadingPreferencesProvider (App.tsx), здесь оборачиваем локально,
  // как делает MarkdownEditor.test.tsx.
  return render(
    <ReadingPreferencesProvider>
      <MemoryRouter initialEntries={['/works/7/pages/3/suggest']}>
        <Routes>
          <Route path="/works/:workId/pages/:pageNumber/suggest" element={<PageSuggest />} />
        </Routes>
      </MemoryRouter>
    </ReadingPreferencesProvider>,
  );
}

beforeEach(() => {
  // Большинству тестов файла нужен вошедший читатель — форма теперь под
  // входом; тест на приглашение записаться сам переопределяет это состояние.
  useAuth.setState({ user: READER_USER, token: 't', isAuthenticated: true, isLoading: false });
});

describe('PageSuggest', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('зовёт записаться, если читатель не вошёл', async () => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    vi.spyOn(pagesApi, 'getByNumber').mockReturnValue(ok(PAGE) as never);
    vi.spyOn(worksApi, 'get').mockReturnValue(
      ok({ id: 7, title: 'Том 8', page_offset: 0 }) as never,
    );
    const create = vi.spyOn(suggestionsApi, 'create');

    render(
      <ReadingPreferencesProvider>
        <MemoryRouter initialEntries={['/works/7/pages/3/suggest']}>
          <Routes>
            <Route path="/works/:workId/pages/:pageNumber/suggest" element={<PageSuggest />} />
          </Routes>
        </MemoryRouter>
      </ReadingPreferencesProvider>,
    );

    const link = await screen.findByRole('link', { name: /записаться/i });
    expect(link).toHaveAttribute('href', '/join?next=%2Fworks%2F7%2Fpages%2F3%2Fsuggest');
    // Форма не показана и запрос страницы за ней не идёт вовсе.
    expect(create).not.toHaveBeenCalled();
  });

  it('предзаполняет поле текущим текстом полосы', async () => {
    renderSuggest();
    expect(await screen.findByDisplayValue('Текст полосы до правки')).toBeInTheDocument();
  });

  it('409 не стирает текст читателя', async () => {
    const user = userEvent.setup();
    vi.spyOn(suggestionsApi, 'create').mockRejectedValue({
      response: { status: 409, data: { message: 'Полосу успели поправить' } },
    });

    renderSuggest();
    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Мой исправленный текст');
    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => {
      expect(screen.getByText(/успели поправить/i)).toBeInTheDocument();
    });
    // Главное: работа читателя на месте. Потерять её — значит потерять и
    // читателя: второй раз он набирать не станет.
    expect(screen.getByDisplayValue('Мой исправленный текст')).toBeInTheDocument();
  });

  it('называет полосу явно, чтобы промах был виден до отправки', async () => {
    renderSuggest();
    expect(await screen.findByText(/страница 3/i)).toBeInTheDocument();
  });

  // Единственный путь назад к месту чтения без браузерной кнопки «назад» —
  // ссылка в заголовке должна вести на саму полосу, а не только на том.
  it('заголовок — хлебная крошка: ссылка на полосу ведёт на /works/{id}/pages/{n}, а не только на том', async () => {
    renderSuggest();
    await screen.findByDisplayValue('Текст полосы до правки');

    expect(screen.getByRole('link', { name: 'Том 8' })).toHaveAttribute('href', '/works/7');
    expect(screen.getByRole('link', { name: /страница 3/i })).toHaveAttribute(
      'href',
      '/works/7/pages/3',
    );
  });

  it('предпросмотр набранного текста санитизирует HTML — эшелон обороны на случай вставки чужой разметки', async () => {
    const user = userEvent.setup();
    const { container } = renderSuggest();

    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Свой текст <script>window.__pwned = true</script> дальше');

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown script')).toBeNull();
    });
    expect((window as unknown as { __pwned?: boolean }).__pwned).toBeUndefined();
  });

  it('«обновить основу» после 409 не трогает текст читателя и обновляет base', async () => {
    const user = userEvent.setup();
    const FRESH_TEXT = 'Текст полосы обновлён на сервере';
    const freshHash = await pageSha256(FRESH_TEXT);

    vi.spyOn(pagesApi, 'getByNumber')
      .mockReturnValueOnce(ok(PAGE) as never)
      .mockReturnValueOnce(ok({ ...PAGE, content_markdown: FRESH_TEXT }) as never);
    vi.spyOn(worksApi, 'get').mockReturnValue(
      ok({ id: 7, title: 'Том 8', page_offset: 0 }) as never,
    );
    // Успех только если пришла та самая, обновлённая основа — иначе это не
    // подтверждает, что «обновить основу» действительно её обновила, а не
    // просто спрятала предупреждение.
    const create = vi
      .spyOn(suggestionsApi, 'create')
      .mockImplementation((_workId, _pageId, data) =>
        data.base_sha256 === freshHash
          ? (ok({ id: 1, status: 'новое' }) as never)
          : Promise.reject({
              response: { status: 409, data: { message: 'Полосу успели поправить' } },
            }),
      );

    render(
      <ReadingPreferencesProvider>
        <MemoryRouter initialEntries={['/works/7/pages/3/suggest']}>
          <Routes>
            <Route path="/works/:workId/pages/:pageNumber/suggest" element={<PageSuggest />} />
          </Routes>
        </MemoryRouter>
      </ReadingPreferencesProvider>,
    );

    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Мой исправленный текст');
    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => {
      expect(screen.getByText(/успели поправить/i)).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /обновить основу/i }));

    await waitFor(() => {
      expect(screen.getByText(FRESH_TEXT)).toBeInTheDocument();
    });
    // Текст читателя в поле — тот же, что он набрал, «обновить основу» его
    // не задела.
    expect(screen.getByDisplayValue('Мой исправленный текст')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => {
      expect(screen.getByText(/отправлено/i)).toBeInTheDocument();
    });
    expect(create).toHaveBeenLastCalledWith(
      7,
      42,
      expect.objectContaining({ base_sha256: freshHash }),
    );
  });

  // Проводка ловушки от поля формы до тела запроса. Этот самый дефект в ветке
  // уже случался: клиент отправлял жёстко вписанную пустую строку, и ловушка
  // не могла сработать никогда — сервер её проверяет, а нечего было
  // проверять. Тестом это не ловилось вовсе (grep binding_ref по этому файлу
  // не находил ничего).
  it('пустая ловушка уезжает на сервер пустой', async () => {
    const user = userEvent.setup();
    const create = vi
      .spyOn(suggestionsApi, 'create')
      .mockReturnValue(ok({ id: 1, status: 'новое' }) as never);

    renderSuggest();
    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Мой исправленный текст');
    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(create).toHaveBeenCalledWith(
      7,
      42,
      expect.objectContaining({ binding_ref: '', proposed_markdown: 'Мой исправленный текст' }),
    );
  });

  it('заполненная ловушка уезжает на сервер заполненной', async () => {
    const user = userEvent.setup();
    const create = vi
      .spyOn(suggestionsApi, 'create')
      .mockReturnValue(ok({ id: 1, status: 'новое' }) as never);

    const { container } = renderSuggest();
    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Мой исправленный текст');

    // Поле уведено с экрана (position: absolute; left: -10000px) и не имеет
    // подписи — человек его не видит и табом не достаёт. Бот, заполняющий
    // все поля формы подряд, — достаёт; именно это здесь и разыграно.
    const trap = container.querySelector<HTMLInputElement>('input[name="binding_ref"]');
    expect(trap).not.toBeNull();
    await user.type(trap as HTMLInputElement, 'бот заполнил всё');

    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(create).toHaveBeenCalledWith(
      7,
      42,
      expect.objectContaining({ binding_ref: 'бот заполнил всё' }),
    );
  });

  // Сервер больше не читает reader_key вовсе — поле не должно уезжать в теле
  // подачи, даже пустым: пустое поле молчаливо намекает, что механизм ещё
  // где-то жив, а лишний ключ в теле запроса — это в точности то, что билет
  // раньше и делал.
  it('подача правки не посылает reader_key', async () => {
    const user = userEvent.setup();
    const create = vi
      .spyOn(suggestionsApi, 'create')
      .mockReturnValue(ok({ id: 1, status: 'новое' }) as never);

    renderSuggest();
    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Мой исправленный текст');
    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    const body = create.mock.calls[0][2] as Record<string, unknown>;
    expect(body).not.toHaveProperty('reader_key');
  });

  it('после отправки ведёт на «моё», билет не показывает', async () => {
    const user = userEvent.setup();
    vi.spyOn(suggestionsApi, 'create').mockReturnValue(ok({ id: 1, status: 'новое' }) as never);

    renderSuggest();
    const field = await screen.findByDisplayValue('Текст полосы до правки');
    await user.clear(field);
    await user.type(field, 'Мой исправленный текст');
    await user.click(screen.getByRole('button', { name: /предложить/i }));

    await waitFor(() => expect(screen.getByText(/отправлено/i)).toBeInTheDocument());
    const link = screen.getByRole('link', { name: /мои предложения/i });
    expect(link).toHaveAttribute('href', '/mine');
    expect(screen.queryByRole('button', { name: /скопировать/i })).toBeNull();
  });
});
