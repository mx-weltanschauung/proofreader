import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import type { AxiosResponse } from 'axios';
import api, { audioApi, conceptsApi, editionsApi, readingApi, searchApi } from './api';
import { EMPTY_SCOPE, type SearchScope } from '../utils/searchScope';

/**
 * axios не даёт достучаться до зарегистрированного interceptor'а через
 * публичный API — только через внутреннее поле `handlers` у
 * InterceptorManager (в типах axios не объявлено). Без `axios-mock-adapter`
 * (в зависимостях его нет, добавлять не будем) и без изменений в
 * production-коде это единственный способ дёрнуть именно rejected-обработчик
 * 401-перехватчика напрямую, без реального HTTP-запроса.
 */
interface ResponseInterceptorHandler {
  rejected: (error: unknown) => Promise<unknown>;
}
interface InterceptorManagerWithHandlers {
  handlers: ResponseInterceptorHandler[];
}

function get401Handler(): ResponseInterceptorHandler['rejected'] {
  const manager = api.interceptors.response as unknown as InterceptorManagerWithHandlers;
  const handler = manager.handlers[0];
  if (!handler?.rejected) {
    throw new Error('401-перехватчик не зарегистрирован — сломался порядок инициализации api.ts');
  }
  return handler.rejected;
}

function rejectWith401() {
  return get401Handler()({ response: { status: 401 } }).catch(() => {
    // Перехватчик всегда пробрасывает ошибку дальше — это ожидаемо,
    // в тесте нас интересуют только его побочные эффекты.
  });
}

describe('401-перехватчик', () => {
  const originalLocation = window.location;

  beforeEach(() => {
    localStorage.clear();
    Object.defineProperty(window, 'location', {
      value: { pathname: '/works/6', search: '', href: '' },
      writable: true,
      configurable: true,
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    });
  });

  it('уводит на /login с next, указывающим на адрес до 401', async () => {
    window.location.pathname = '/works/6';
    window.location.search = '?tab=info';

    await rejectWith401();

    expect(window.location.href).toBe('/login?next=%2Fworks%2F6%3Ftab%3Dinfo');
  });

  it('не трогает адрес, если уже на /login — иначе петля редиректов', async () => {
    window.location.pathname = '/login';
    window.location.search = '';

    await rejectWith401();

    expect(window.location.href).toBe('');
  });

  // У читателя тоже bearer-токен: по истечении сессии сотрудническая дверь
  // не годится — она просит почту, которой у читателя нет, и выхода с
  // экрана нет.
  it('читателя по истечении сессии уводит на /join, а не на /login', async () => {
    window.location.pathname = '/works/6/pages/493/suggest';
    window.location.search = '';
    localStorage.setItem(
      'auth-storage',
      JSON.stringify({
        state: {
          user: { id: 5, email: '', nickname: 'Читатель', role: 'reader' },
          token: 't',
          isAuthenticated: true,
        },
        version: 0,
      }),
    );

    await rejectWith401();

    expect(window.location.href).toBe('/join?next=%2Fworks%2F6%2Fpages%2F493%2Fsuggest');
  });

  it('не трогает адрес, если читатель уже на /join — иначе петля редиректов', async () => {
    window.location.pathname = '/join';
    window.location.search = '';
    localStorage.setItem(
      'auth-storage',
      JSON.stringify({
        state: { user: { id: 5, email: '', nickname: 'Читатель', role: 'reader' } },
        version: 0,
      }),
    );

    await rejectWith401();

    expect(window.location.href).toBe('');
  });

  it('сотрудника по истечении сессии по-прежнему уводит на /login', async () => {
    window.location.pathname = '/admin/users';
    window.location.search = '';
    localStorage.setItem(
      'auth-storage',
      JSON.stringify({
        state: { user: { id: 1, email: 'a@b.c', role: 'administrator' } },
        version: 0,
      }),
    );

    await rejectWith401();

    expect(window.location.href).toBe('/login?next=%2Fadmin%2Fusers');
  });

  // Первый заход (роли в хранилище ещё нет) — прежнее поведение, на /login.
  it('без сохранённой роли ведёт на /login, как раньше', async () => {
    window.location.pathname = '/works/6';
    window.location.search = '';

    await rejectWith401();

    expect(window.location.href).toBe('/login?next=%2Fworks%2F6');
  });

  it('испорченная запись в auth-storage не роняет перехватчик — ведёт на /login', async () => {
    window.location.pathname = '/works/6';
    window.location.search = '';
    localStorage.setItem('auth-storage', '{не json');

    await rejectWith401();

    expect(window.location.href).toBe('/login?next=%2Fworks%2F6');
  });
});

describe('клиенты понятий и собраний', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('навигатор шлёт фильтры параметрами запроса, а не в пути', () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: [] } as unknown as AxiosResponse<unknown>);

    void conceptsApi.list({ q: 'абстр', letter: 'а', limit: 100, offset: 0 });

    expect(get).toHaveBeenCalledWith('/concepts', {
      params: { q: 'абстр', letter: 'а', limit: 100, offset: 0 },
    });
  });

  it('экранирует слаг в пути статьи', () => {
    // Слаги строятся транслитерацией и в норме безопасны, но путь всё равно
    // собирается экранированием: слаг приходит из данных, а не из кода.
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);

    void conceptsApi.get('a b');

    expect(get).toHaveBeenCalledWith('/concepts/a%20b');
  });

  it('обратные ссылки адресуются работой и страницей', () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: [] } as unknown as AxiosResponse<unknown>);

    void conceptsApi.forPage(4, 1001);

    expect(get).toHaveBeenCalledWith('/works/4/pages/1001/concepts');
  });

  it('запрашивает записи понятия с фильтрами и порцией', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { total: 0, entries: [] } });

    await conceptsApi.fragments('abstraktnyj-trud', {
      rubric: 'его мера',
      volume: 25,
      volume_part: 'II',
      limit: 20,
      offset: 40,
    });

    expect(get).toHaveBeenCalledWith('/concepts/abstraktnyj-trud/fragments', {
      params: { rubric: 'его мера', volume: 25, volume_part: 'II', limit: 20, offset: 40 },
    });
  });

  // Задача 13a: reference_id сужает поток до одной записи — им пользуется
  // точечное обновление после сохранения границ (useConceptFragments.replaceEntry).
  it('запрашивает записи понятия по одному адресу', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { total: 1, entries: [] } });

    await conceptsApi.fragments('abstraktnyj-trud', { reference_id: 473, limit: 1 });

    expect(get).toHaveBeenCalledWith('/concepts/abstraktnyj-trud/fragments', {
      params: { reference_id: 473, limit: 1 },
    });
  });

  it('разворачивает страницу записи по адресу, ссылке и странице', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        page_id: 1001,
        page_number: 5,
        printed_page: 730,
        page_status: 'вычитана',
        markdown: '',
        chunks: [],
      },
    });

    await conceptsApi.expandPage('abstraktnyj-trud', 473, 1001);

    expect(get).toHaveBeenCalledWith('/concepts/abstraktnyj-trud/references/473/pages/1001');
  });

  it('шлёт правку границ вырезок целиком, а не по одной', async () => {
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { state: 'fragment', cuts: [] } });

    await conceptsApi.putCuts('abstraktnyj-trud', 473, {
      status: 'confirmed',
      cuts: [{ start_page: 5, start_offset: 12, end_page: 5, end_offset: 40 }],
    });

    expect(put).toHaveBeenCalledWith('/concepts/abstraktnyj-trud/references/473/cuts', {
      status: 'confirmed',
      cuts: [{ start_page: 5, start_offset: 12, end_page: 5, end_offset: 40 }],
    });
  });

  // Адрес собран из id, а промах в шаблоне строки молча уводит запрос в
  // 404 — а вызывающий код увидит просто пустую полку.
  it('запрашивает тома собрания по его id', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: [] } as unknown as AxiosResponse<unknown>);

    await editionsApi.volumes(1);

    expect(get).toHaveBeenCalledWith('/editions/1/works');
  });

  it('запрашивает список собраний', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: [] } as unknown as AxiosResponse<unknown>);

    await editionsApi.list();

    expect(get).toHaveBeenCalledWith('/editions');
  });

  it('читает одно собрание по id', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);

    await editionsApi.get(1);

    expect(get).toHaveBeenCalledWith('/editions/1');
  });
});

describe('потоковое чтение', () => {
  function stubWindow() {
    return vi.spyOn(api, 'get').mockResolvedValue({
      data: { pages: [], next_from: null, total_pages: 0 },
    } as unknown as AxiosResponse<unknown>);
  }

  it('запрашивает окно с указанного номера', async () => {
    const get = stubWindow();

    await readingApi.window(47, 43);

    expect(get).toHaveBeenCalledWith('/works/47/reading', { params: { from: 43 } });
  });

  // Сервер принимает count и зажимает его в [1, 50]; клиент про третий
  // аргумент не знал, и договор разъезжался молча.
  it('передаёт запрошенный размер окна', async () => {
    const get = stubWindow();

    await readingApi.window(47, 43, 25);

    expect(get).toHaveBeenCalledWith('/works/47/reading', { params: { from: 43, count: 25 } });
  });

  // Без count решает сервер: параметра в запросе быть не должно, иначе
  // undefined уехал бы в строку запроса как пустое значение.
  it('без размера окна параметра нет вовсе', async () => {
    const get = stubWindow();

    await readingApi.window(47, 43);

    const params = get.mock.calls[0][1]?.params as Record<string, unknown>;
    expect('count' in params).toBe(false);
  });
});

// Находка 6 итоговой рецензии: сериализация области поиска в параметры
// запроса (запятая между id, пустой список — параметра нет вовсе) нигде не
// проверялась напрямую — тесты Search.tsx мокали сам searchApi и не видели,
// что уходит по проводу. Здесь мокается `api.get` — тот же приём, что и в
// блоке «клиенты понятий и собраний» выше, — поэтому проверяется настоящая
// сборка params из searchApi.search/pages, а не поведение обёртки.
describe('сериализация области поиска в параметры', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('пустая область не добавляет editions и works к запросу обзора', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);

    await searchApi.search('маркс', EMPTY_SCOPE);

    expect(get).toHaveBeenCalledWith('/search', { params: { q: 'маркс' } });
  });

  it('непустая область склеивает editions и works запятой', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);
    const scope: SearchScope = { editions: [5, 7], works: [13, 25, 99], chapters: [] };

    await searchApi.search('маркс', scope);

    expect(get).toHaveBeenCalledWith('/search', {
      params: { q: 'маркс', editions: '5,7', works: '13,25,99' },
    });
  });

  it('область без editions, но с works — editions в параметрах не появляется', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);
    const scope: SearchScope = { editions: [], works: [13], chapters: [] };

    await searchApi.search('маркс', scope);

    const params = get.mock.calls[0][1]?.params as Record<string, unknown>;
    expect('editions' in params).toBe(false);
    expect(params.works).toBe('13');
  });

  it('без выбранных глав запрос полос уходит без параметра chapters', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);

    await searchApi.pages({ q: 'маркс', work_id: 13 });

    const params = get.mock.calls[0][1]?.params as Record<string, unknown>;
    expect('chapters' in params).toBe(false);
    expect(params).toMatchObject({ q: 'маркс', work_id: 13 });
  });

  it('выбранные главы склеиваются запятой в запросе полос', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);

    await searchApi.pages({ q: 'маркс', work_id: 13, chapters: [101, 102, 5] });

    expect(get).toHaveBeenCalledWith('/search/pages', {
      params: {
        q: 'маркс',
        work_id: 13,
        limit: undefined,
        offset: undefined,
        chapters: '101,102,5',
      },
    });
  });
});

describe('audioApi', () => {
  afterEach(() => vi.restoreAllMocks());

  it('ходит по маршрутам озвучки', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);
    const patch = vi
      .spyOn(api, 'patch')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);
    const del = vi
      .spyOn(api, 'delete')
      .mockResolvedValue({ data: {} } as unknown as AxiosResponse<unknown>);

    await audioApi.forWork(4);
    await audioApi.queue();
    await audioApi.enqueue(4, 223);
    await audioApi.enqueue(4);
    await audioApi.cancel(7);
    await audioApi.retry(7);
    await audioApi.requeueStale(4);
    await audioApi.recordingUploadURL(4, 223, { content_type: 'audio/mpeg', bytes: 10 });
    await audioApi.registerRecording(4, 223, {
      key: 'k',
      content_type: 'audio/mpeg',
      bytes: 10,
      duration_ms: 1000,
      reader: 'Иванов',
    });
    await audioApi.updateRecording(9, { position: 2 });
    await audioApi.deleteRecording(9);

    expect(get.mock.calls.map((c) => c[0])).toEqual(['/works/4/audio', '/audio/queue']);
    // toStrictEqual: toEqual не отличает {} от { chapter_id: undefined }.
    expect(post.mock.calls).toStrictEqual([
      ['/works/4/audio/queue', { chapter_id: 223 }],
      // Заявка на том — пустое тело, а не chapter_id: undefined.
      ['/works/4/audio/queue', {}],
      ['/audio/queue/7/retry'],
      ['/works/4/audio/requeue-stale'],
      ['/works/4/chapters/223/recordings/uploads', { content_type: 'audio/mpeg', bytes: 10 }],
      [
        '/works/4/chapters/223/recordings',
        { key: 'k', content_type: 'audio/mpeg', bytes: 10, duration_ms: 1000, reader: 'Иванов' },
      ],
    ]);
    expect(patch.mock.calls).toEqual([['/audio/rec/9', { position: 2 }]]);
    expect(del.mock.calls.map((c) => c[0])).toEqual(['/audio/queue/7', '/audio/rec/9']);
  });
});
