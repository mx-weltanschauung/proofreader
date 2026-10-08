import axios from 'axios';
import { loginPathFor, joinPathFor } from '../utils/returnUrl';
import { EMPTY_SCOPE, type SearchScope } from '../utils/searchScope';
import type {
  AudioQueue,
  AudioQueueItem,
  AudioRecording,
  AuthResponse,
  User,
  Work,
  WorkAudio,
  RecordingUpload,
  Category,
  Page,
  PageMapEntry,
  PageVersion,
  Document,
  DocumentCut,
  Chapter,
  ChapterPage,
  NoteIndexEntry,
  AdminUser,
  AdminReader,
  AdminReaderCollection,
  UserRole,
  Edition,
  Shelf,
  StaticArchive,
  EditionHighlight,
  VolumeSummary,
  Concept,
  ConceptSummary,
  ConceptBacklink,
  ConceptEntriesPage,
  ConceptEntry,
  ConceptOrder,
  ConceptCut,
  ConceptCutInput,
  ConceptExpandedPage,
  WorkUpdatePayload,
  Collection,
  CollectionItem,
  ReadingWindow,
  SuggestionRow,
  SuggestionDetail,
  SuggestionStatus,
  RejectReason,
  DocumentRejectReason,
  PageBlock,
  Feedback,
  SearchResponse,
  SearchTermsResponse,
  SearchPagesResponse,
  CacheStats,
  StatsTrafficRow,
  StatsTopRow,
  StatsTopKind,
  StatsCountRow,
  StatsSearches,
  StatsCrawlerRow,
  StatsDeviceRow,
  StatsHealth,
} from '../types';

// Get API base URL from environment variable
// In development with Vite proxy, use relative URLs
// In production or if VITE_API_BASE_URL is set, use full URL
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '';

const api = axios.create({
  baseURL: API_BASE_URL ? `${API_BASE_URL}/api` : '/api',
});

// Add auth token to requests
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Читатель тоже носит bearer-токен теперь, а не только сотрудник — по
// истечении сессии его нельзя вести на сотрудническую дверь (/login просит
// почту, которой у читателя нет, и с этого экрана нет выхода). Роль
// смотрим в персистентом сторе zustand ДО его очистки: после removeItem
// её уже не прочитать.
function storedUserRole(): string | null {
  try {
    const raw = localStorage.getItem('auth-storage');
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { state?: { user?: { role?: string } | null } };
    return parsed.state?.user?.role ?? null;
  } catch {
    // Испорченное значение в localStorage — не повод падать здесь; ведём как
    // будто роли нет вовсе (прежнее поведение, на /login).
    return null;
  }
}

// Handle 401 errors - logout automatically
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // Роли без сохранённого пользователя (первый заход) достаётся прежнее
      // поведение — на /login, хуже нынешнего не будет.
      const isReader = storedUserRole() === 'reader';
      localStorage.removeItem('token');
      // Also clear the persisted auth store (zustand persist key), otherwise the
      // route guard still sees isAuthenticated=true and bounces back to /works,
      // causing an infinite 401 redirect loop once the token expires.
      localStorage.removeItem('auth-storage');
      const doorPath = isReader ? '/join' : '/login';
      if (window.location.pathname !== doorPath) {
        window.location.href = isReader
          ? joinPathFor(window.location.pathname, window.location.search)
          : loginPathFor(window.location.pathname, window.location.search);
      }
    }
    return Promise.reject(error);
  },
);

// Auth API
export const authApi = {
  login: (email: string, password: string) =>
    api.post<AuthResponse>('/auth/login', { email, password }),

  // Вход читателя равен регистрации: нет такого ника — заводится.
  join: (nickname: string, password: string) =>
    api.post<AuthResponse>('/auth/reader', { nickname, password }),

  // token — продлённая читательская сессия: сервер шлёт свежий токен, когда
  // текущему больше недели. Сотруднику это поле не приходит никогда.
  me: () => api.get<User & { token?: string }>('/auth/me'),

  // Уход читателя. Сервер отказывает сотруднику 403 (см. DeleteMe в
  // internal/api/auth_handler.go); токен при этом не отзывается — он просто
  // выброшен на клиенте вызовом logout() из useAuth сразу после успеха.
  deleteMe: () => api.delete('/auth/me'),
};

// Users API
export const usersApi = {
  list: () => api.get<AdminUser[]>('/users'),

  create: (payload: { email: string; password: string; role: UserRole }) =>
    api.post<AdminUser>('/users', payload),

  update: (id: number, payload: { role?: UserRole; password?: string }) =>
    api.put<AdminUser>(`/users/${id}`, payload),

  remove: (id: number) => api.delete(`/users/${id}`),
};

// Читатели глазами администратора — отдельная дверь от usersApi: там
// сотрудники, здесь читатели, и смешивать списки нельзя. Роли, пароля и
// удаления здесь нет намеренно (см. ReadersList).
export const readersApi = {
  list: (q?: string) => api.get<AdminReader[]>('/readers', { params: q ? { q } : undefined }),

  // Все подборки ника: черновики, снятые и опубликованные. Ник — снимок
  // подписи, поэтому находятся и осиротевшие подборки удалённого читателя,
  // у которых владельца уже нет.
  collections: (nickname: string) =>
    api.get<AdminReaderCollection[]>(`/readers/${encodeURIComponent(nickname)}/collections`),
};

// Feedback API
export const feedbackApi = {
  send: (payload: { message: string; source_path: string; binding_ref: string }) =>
    api.post<{ ok: boolean }>('/feedback', payload),

  list: (handled?: boolean) =>
    api.get<Feedback[]>('/feedback', {
      params: handled === undefined ? undefined : { handled },
    }),

  unreadCount: () => api.get<{ count: number }>('/feedback/unread-count'),

  setHandled: (id: number, handled: boolean) =>
    api.patch<{ ok: boolean }>(`/feedback/${id}`, { handled }),

  remove: (id: number) => api.delete(`/feedback/${id}`),
};

// Works API
export const worksApi = {
  list: (params?: { limit?: number; offset?: number }) => api.get<Work[]>('/works', { params }),

  get: (id: number) => api.get<Work>(`/works/${id}`),

  /** Номера и статусы всех страниц. Лёгкая замена pagesApi.list там, где
   *  нужен только статус: на томе в 840 страниц список отдаёт 4.2 МБ. */
  pageMap: (id: number) => api.get<PageMapEntry[]>(`/works/${id}/page-map`),

  create: (data: Partial<Work>) => api.post<Work>('/works', data),

  update: (id: number, data: WorkUpdatePayload) => api.put<Work>(`/works/${id}`, data),

  delete: (id: number) => api.delete(`/works/${id}`),

  uploadFile: (id: number, formData: FormData) =>
    api.post(`/works/${id}/upload`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    }),

  createPages: (id: number, pageRange?: string) =>
    api.post<{ created_pages: number; requested_pages: number }>(`/works/${id}/create-pages`, {
      page_range: pageRange || '',
    }),

  addCategory: (workId: number, categoryId: number) =>
    api.post(`/works/${workId}/categories`, { category_id: categoryId }),

  removeCategory: (workId: number, categoryId: number) =>
    api.delete(`/works/${workId}/categories/${categoryId}`),
};

// Categories API
export const categoriesApi = {
  list: () => api.get<Category[]>('/categories'),

  get: (id: number) => api.get<Category>(`/categories/${id}`),

  create: (data: Partial<Category>) => api.post<Category>('/categories', data),

  update: (id: number, data: Partial<Category>) => api.put<Category>(`/categories/${id}`, data),

  delete: (id: number) => api.delete(`/categories/${id}`),
};

// Pages API
export const pagesApi = {
  list: (workId: number) => api.get<Page[]>(`/works/${workId}/pages`),

  get: (workId: number, pageId: number) => api.get<Page>(`/works/${workId}/pages/${pageId}`),

  getByNumber: (workId: number, pageNumber: number) =>
    api.get<Page>(`/works/${workId}/pages/by-number/${pageNumber}`),

  update: (workId: number, pageId: number, data: Partial<Page>) =>
    api.put<Page>(`/works/${workId}/pages/${pageId}`, data),

  render: (workId: number, pageId: number) =>
    api.get<{ html: string }>(`/works/${workId}/pages/${pageId}/render`),

  listVersions: (workId: number, pageId: number) =>
    api.get<PageVersion[]>(`/works/${workId}/pages/${pageId}/versions`),

  getVersion: (workId: number, pageId: number, versionId: number) =>
    api.get<PageVersion>(`/works/${workId}/pages/${pageId}/versions/${versionId}`),

  restoreVersion: (workId: number, pageId: number, versionId: number) =>
    api.post<Page>(`/works/${workId}/pages/${pageId}/versions/${versionId}/restore`),

  /** Полоса, разрезанная на блоки: поверхность выбора для подборщика
   *  вклейки. Читателю сырой markdown не показывают. */
  blocks: (workId: number, pageId: number) =>
    api.get<{ blocks: PageBlock[] }>(`/works/${workId}/pages/${pageId}/blocks`),
};

// Chapters API
export const chaptersApi = {
  list: (workId: number) => api.get<Chapter[]>(`/works/${workId}/chapters`),

  get: (workId: number, chapterId: number) =>
    api.get<Chapter>(`/works/${workId}/chapters/${chapterId}`),

  create: (workId: number, data: Partial<Chapter>) =>
    api.post<Chapter>(`/works/${workId}/chapters`, data),

  update: (workId: number, chapterId: number, data: Partial<Chapter>) =>
    api.put<Chapter>(`/works/${workId}/chapters/${chapterId}`, data),

  delete: (workId: number, chapterId: number) =>
    api.delete(`/works/${workId}/chapters/${chapterId}`),

  move: (
    workId: number,
    chapterId: number,
    data: { parent_id?: number | null; order_number: number },
  ) => api.patch<Chapter>(`/works/${workId}/chapters/${chapterId}/move`, data),

  listPages: (workId: number, chapterId: number) =>
    api.get<{ pages: ChapterPage[]; footnotes_html: string }>(
      `/works/${workId}/chapters/${chapterId}/pages`,
    ),
};

// Потоковое чтение: окно страниц работы. Размер окна по умолчанию выбирает
// сервер, и читалка его не задаёт — но параметр в договоре есть (сервер
// зажимает его в [1, 50]), и клиент, который о нём не знает, тихо расходится
// с сервером. Без count параметр в запрос не попадает вовсе: undefined уехал
// бы в строку запроса пустым значением.
export const readingApi = {
  window: (workId: number, from: number, count?: number) =>
    api.get<ReadingWindow>(`/works/${workId}/reading`, {
      params: count === undefined ? { from } : { from, count },
    }),
};

// Notes API
export const notesApi = {
  list: (workId: number) => api.get<NoteIndexEntry[]>(`/works/${workId}/notes`),
};

// Адрес разбора: короткий (/documents/{slug}) у сотруднического, длинный
// (/documents/{ник}/{slug}) у читательского — то же правило и по той же
// причине, что у collectionUrl ниже (сервер под коротким видом ищет только
// author_nickname = ''). Ключ идёт объектом, а не парой позиционных
// аргументов, как у collectionUrl: слаг у разбора не украшение, а часть
// ключа вместе с ником, и потребители (DocumentView/DocumentForm через
// useParams, очередь модерации через саму строку разбора) уже держат обе
// части вместе — собирать их в позиционную пару и обратно незачем.
export interface DocumentKey {
  nickname?: string;
  slug: string;
}

function documentApiPath(key: DocumentKey, suffix = ''): string {
  const base = key.nickname
    ? `/documents/${encodeURIComponent(key.nickname)}/${encodeURIComponent(key.slug)}`
    : `/documents/${encodeURIComponent(key.slug)}`;
  return `${base}${suffix}`;
}

// Documents API
export const documentsApi = {
  list: (params?: { limit?: number; offset?: number }) =>
    api.get<Document[]>('/documents', { params }),

  get: (key: DocumentKey) => api.get<Document>(documentApiPath(key)),

  create: (data: { title: string; markdown_content: string }) =>
    api.post<Document>('/documents', data),

  update: (key: DocumentKey, data: { title: string; markdown_content: string }) =>
    api.put<Document>(documentApiPath(key), data),

  delete: (key: DocumentKey) => api.delete(documentApiPath(key)),

  view: (key: DocumentKey) =>
    api.get<{
      id: number;
      slug: string;
      title: string;
      html_content: string;
      owner_id: number | null;
      /** Снимок подписи — пусто у сотруднического разбора. */
      author_nickname: string;
      /** Не null — разбор на людях (documentVisibleTo пропустил бы иначе). */
      published_at: string | null;
      created_at: string;
      updated_at: string;
      /** Поля состояния — для полосы состояния над разбором (M7).
       *  Постороннему `documentForViewer` гасит ТОЛЬКО `review_status` (в
       *  `""`); `was_published` не гасится ни здесь, ни в карточке — по нему
       *  читатель отличает снятое от небывшего, и это публичное сведение. */
      review_status: string;
      was_published: boolean;
      /** Черновик разошёлся с тем, что на людях: разбор опубликован, автор
       *  сохранил правку, но на проверку не отправил — шестое состояние,
       *  которого перечень не знал. Считает сервер: обе редакции есть только
       *  у него и только у того, кто вправе их видеть. */
      has_unpublished_changes: boolean;
    }>(documentApiPath(key, '/view')),

  // Остаток подрезанной вклейки — вклейка целиком, тем же ordinal, что и при
  // сборке разбора (см. cutOrdinal на сервере). Возвращает готовый HTML для
  // вставки на место подрезанного блока.
  cut: (key: DocumentKey, cutId: number) =>
    api.get<{ html: string }>(documentApiPath(key, `/cuts/${cutId}`)),

  createCut: (
    key: DocumentKey,
    body: {
      work_id: number;
      start_page: number;
      start_offset: number;
      end_page: number;
      end_offset: number;
      source_title: string;
    },
  ) => api.post<DocumentCut>(documentApiPath(key, '/cuts'), body),

  deleteCut: (key: DocumentKey, cutId: number) =>
    api.delete(documentApiPath(key, `/cuts/${cutId}`)),

  /** Свои разборы — черновики, поданное, одобренное и отклонённое с
   *  причиной. Требует входа. */
  mine: () => api.get<Document[]>('/documents/mine'),

  /** Отправить черновик на проверку. У сотрудника публикует сразу. */
  submit: (key: DocumentKey) => api.post<Document>(documentApiPath(key, '/submit')),

  /** Убрать с людей, оставив себе черновик. Владельцу и редакции. */
  unpublish: (key: DocumentKey) => api.post<Document>(documentApiPath(key, '/unpublish')),

  /** Очередь модерации — редактору. Отдаётся без фильтра
   *  `documentForViewer`: модератор судит о том, что подали, целиком. */
  review: () => api.get<Document[]>('/documents/review'),

  /** Одобрение и отказ отвечают ПОЛНЫМ состоянием, без фильтра постороннего
   *  — иначе модератор не увидел бы исхода собственного действия
   *  (`respondWithOwnDecision` на сервере, в отличие от `respondWith` у
   *  submit/unpublish). */
  approve: (key: DocumentKey) => api.post<Document>(documentApiPath(key, '/approve')),

  reject: (key: DocumentKey, reason: DocumentRejectReason) =>
    api.post<Document>(documentApiPath(key, '/reject'), { reason }),
};

// Editions API — собрания сочинений и их тома
export const editionsApi = {
  // Списки приходят из репозиториев, которые накапливают строки в nil-слайс,
  // так что на пустой выборке в теле лежит null, а не []. Потребители обязаны
  // писать `?? []` — тип об этом напоминает.
  list: () => api.get<Edition[] | null>('/editions'),

  get: (id: number) => api.get<Edition>(`/editions/${id}`),

  volumes: (editionId: number) => api.get<VolumeSummary[] | null>(`/editions/${editionId}/works`),

  highlights: (editionId: number) =>
    api.get<EditionHighlight[]>(`/editions/${editionId}/highlights`),

  // Список целиком: перестановка, добавление и удаление — один запрос.
  saveHighlights: (editionId: number, items: { chapter_id: number; label: string }[]) =>
    api.put<EditionHighlight[]>(`/editions/${editionId}/highlights`, items),

  create: (data: { title: string; slug: string; url_slug: string; description: string }) =>
    api.post<Edition>('/editions', data),

  update: (
    id: number,
    data: { title: string; slug: string; url_slug: string; description: string },
  ) => api.put<Edition>(`/editions/${id}`, data),

  remove: (id: number) => api.delete(`/editions/${id}`),
};

// Главная целиком — собрания со своими томами и работы вне собраний.
// Списки внутри ответа хендлер выправляет сам, поэтому null тут не бывает.
export const shelfApi = {
  get: () => api.get<Shelf>('/shelf'),
};

// Вся читальня одним архивом: сведения о текущей сборке. 404 — архива сейчас
// нет (пересобирается), 503 — бакет не ответил.
export const staticArchiveApi = {
  get: () => api.get<StaticArchive>('/static-archive'),
};

// Полнотекстовый поиск. Списки в ответе сервер выправляет сам ([]), null не
// бывает; 400 на пустой запрос, 503 на таймаут — оба с {message}.
export const searchApi = {
  search: (q: string, scope: SearchScope = EMPTY_SCOPE) =>
    api.get<SearchResponse>('/search', {
      params: {
        q,
        ...(scope.editions.length > 0 ? { editions: scope.editions.join(',') } : {}),
        ...(scope.works.length > 0 ? { works: scope.works.join(',') } : {}),
      },
    }),

  terms: (q: string) => api.get<SearchTermsResponse>('/search', { params: { q, terms_only: 1 } }),

  pages: (params: {
    q: string;
    work_id: number;
    chapters?: number[];
    limit?: number;
    offset?: number;
  }) =>
    api.get<SearchPagesResponse>('/search/pages', {
      params: {
        q: params.q,
        work_id: params.work_id,
        limit: params.limit,
        offset: params.offset,
        ...(params.chapters && params.chapters.length > 0
          ? { chapters: params.chapters.join(',') }
          : {}),
      },
    }),
};

// Адрес подборки: короткий (/collections/{slug}) у сотруднической, длинный
// (/collections/{ник}/{slug}) у читательской — сервер под коротким адресом
// ищет только author_nickname = '' (см. GetByAuthorSlug), поэтому нельзя
// просто всегда слать один вид. nickname пуст или не передан — короткий вид.
function collectionUrl(nickname: string | undefined, slug: string, suffix = ''): string {
  const base = nickname
    ? `/collections/${encodeURIComponent(nickname)}/${encodeURIComponent(slug)}`
    : `/collections/${encodeURIComponent(slug)}`;
  return `${base}${suffix}`;
}

// Подборки — главы и работы разных авторов, собранные читателем
export const collectionsApi = {
  // Сервер гарантирует [] на пустой выборке (CollectionHandler.List,
  // застраховано TestCollectionHandlerListNeverReturnsNull) — в отличие от
  // некоторых других списков в этом API. Тип нарочно оставлен допускающим
  // null: ради единообразия с остальными списками проекта потребители всё
  // равно обязаны писать `?? []`.
  list: () => api.get<Collection[] | null>('/collections'),

  // Экран «моё»: черновики и опубликованные подборки вошедшего — свои, и
  // читателя, и сотрудника. Требует токена — без него сервер отвечает 401.
  mine: () => api.get<Collection[]>('/collections/mine'),

  get: (slug: string, nickname?: string) => api.get<Collection>(collectionUrl(nickname, slug)),

  create: (data: { title: string; slug: string; description: string }) =>
    api.post<Collection>('/collections', data),

  update: (
    slug: string,
    data: { title: string; slug: string; description: string },
    nickname?: string,
  ) => api.put<Collection>(collectionUrl(nickname, slug), data),

  remove: (slug: string, nickname?: string) => api.delete(collectionUrl(nickname, slug)),

  publish: (slug: string, nickname?: string) =>
    api.post<Collection>(collectionUrl(nickname, slug, '/publish')),

  unpublish: (slug: string, nickname?: string) =>
    api.post<Collection>(collectionUrl(nickname, slug, '/unpublish')),

  addItem: (
    slug: string,
    data: {
      kind: 'chapter' | 'work';
      chapter_id?: number;
      work_id?: number;
      author_override?: string;
    },
    nickname?: string,
  ) => api.post<CollectionItem>(collectionUrl(nickname, slug, '/items'), data),

  updateItem: (
    slug: string,
    itemId: number,
    data: { author_override: string },
    nickname?: string,
  ) => api.put(collectionUrl(nickname, slug, `/items/${itemId}`), data),

  removeItem: (slug: string, itemId: number, nickname?: string) =>
    api.delete(collectionUrl(nickname, slug, `/items/${itemId}`)),

  moveItem: (slug: string, itemId: number, orderNumber: number, nickname?: string) =>
    api.patch(collectionUrl(nickname, slug, `/items/${itemId}/move`), {
      order_number: orderNumber,
    }),

  // Форма ответа та же, что у страниц главы: постраничные секции плюс сноски.
  itemPages: (slug: string, itemId: number, nickname?: string) =>
    api.get<{ pages: ChapterPage[]; footnotes_html: string }>(
      collectionUrl(nickname, slug, `/items/${itemId}/pages`),
    ),
};

// Concepts API — понятия предметного указателя
export const conceptsApi = {
  list: (params: { q?: string; letter?: string; limit?: number; offset?: number }) =>
    api.get<ConceptSummary[] | null>('/concepts', { params }),

  get: (slug: string) => api.get<Concept>(`/concepts/${encodeURIComponent(slug)}`),

  forPage: (workId: number, pageId: number) =>
    api.get<ConceptBacklink[] | null>(`/works/${workId}/pages/${pageId}/concepts`),

  fragments: (
    slug: string,
    params: {
      rubric?: string;
      /**
       * Путь подрубрики, звенья encodeURIComponent через «:». Побеждает
       * плоский `rubric`, если пришли оба (правило сервера,
       * parseFragmentFilter).
       */
      rubric_path?: string;
      // Порядок чтения потока. Дефолт (по подрубрикам) не посылается вовсе —
      // его знает сервер.
      order?: ConceptOrder;
      volume?: number;
      volume_part?: string;
      // Сужает поток до одного адреса (задача 13a) — точечное обновление
      // записи после сохранения границ перезапрашивает её этим параметром,
      // не трогая офсет и total остального потока.
      reference_id?: number;
      limit?: number;
      offset?: number;
    },
  ) => api.get<ConceptEntriesPage>(`/concepts/${encodeURIComponent(slug)}/fragments`, { params }),

  expandPage: (slug: string, referenceId: number, pageId: number) =>
    api.get<ConceptExpandedPage>(
      `/concepts/${encodeURIComponent(slug)}/references/${referenceId}/pages/${pageId}`,
    ),

  putCuts: (
    slug: string,
    referenceId: number,
    body: { status: 'machine' | 'confirmed'; cuts: ConceptCutInput[] },
  ) =>
    api.put<{ state: ConceptEntry['state']; cuts: ConceptCut[] }>(
      `/concepts/${encodeURIComponent(slug)}/references/${referenceId}/cuts`,
      body,
    ),
};

// Suggestions API — читательские предложения правок
export const suggestionsApi = {
  create: (
    workId: number,
    pageId: number,
    data: {
      proposed_markdown: string;
      note: string;
      base_sha256: string;
      // Honeypot: живой читатель поле не видит и не заполняет — значение
      // должно доехать до сервера пустым. Заполненное значит бота; проводка
      // до сервера — намеренная, не забытый параметр.
      binding_ref: string;
    },
  ) =>
    api.post<{ id: number; status: SuggestionStatus }>(
      `/works/${workId}/pages/${pageId}/suggestions`,
      data,
    ),

  /** Свои правки вошедшего читателя. Токен подставляет общий перехватчик. */
  mine: () => api.get<SuggestionRow[]>('/suggestions/mine'),

  queue: (status: SuggestionStatus | null, limit = 50, offset = 0) =>
    api.get<{ items: SuggestionRow[]; total: number }>('/suggestions', {
      params: { ...(status ? { status } : {}), limit, offset },
    }),

  get: (id: number) => api.get<SuggestionDetail>(`/suggestions/${id}`),

  accept: (id: number, contentMarkdown: string, comment: string) =>
    api.post<{ id: number; status: SuggestionStatus }>(`/suggestions/${id}/accept`, {
      content_markdown: contentMarkdown,
      comment,
    }),

  reject: (id: number, reason: RejectReason) =>
    api.post<{ id: number; status: SuggestionStatus }>(`/suggestions/${id}/reject`, {
      reason,
    }),

  /** Снести отклонённое навсегда. Сервер пускает только отклонённые. */
  remove: (id: number) => api.delete<void>(`/suggestions/${id}`),

  /** Снести все отклонённые разом; отвечает числом снесённых. */
  purgeRejected: () => api.delete<{ deleted: number }>('/suggestions/rejected'),
};

// Файловый кэш готовых глав. Сброс главы доступен редактору, сводка и
// полный сброс — только администратору.
export const cacheApi = {
  stats: () => api.get<CacheStats>('/cache/stats'),

  purgeAll: () => api.delete<{ removed: number; bytes: number }>('/cache'),

  purgeChapter: (workId: number, chapterId: number) =>
    api.delete<{ removed: number }>(`/works/${workId}/chapters/${chapterId}/cache`),
};

// Озвучка (спека аудиокниг 29.09, шаг 3). Чтение — публичное, остальное —
// редактору и администратору.
export const audioApi = {
  forWork: (workId: number) => api.get<WorkAudio>(`/works/${workId}/audio`),

  queue: () => api.get<AudioQueue>('/audio/queue'),

  enqueue: (workId: number, chapterId?: number) =>
    api.post<AudioQueueItem>(
      `/works/${workId}/audio/queue`,
      chapterId === undefined ? {} : { chapter_id: chapterId },
    ),

  cancel: (id: number) => api.delete<void>(`/audio/queue/${id}`),

  retry: (id: number) => api.post<AudioQueueItem>(`/audio/queue/${id}/retry`),

  requeueStale: (workId: number) =>
    api.post<{ queued: number; items: AudioQueueItem[] }>(`/works/${workId}/audio/requeue-stale`),

  recordingUploadURL: (
    workId: number,
    chapterId: number,
    body: { content_type: string; bytes: number },
  ) => api.post<RecordingUpload>(`/works/${workId}/chapters/${chapterId}/recordings/uploads`, body),

  registerRecording: (
    workId: number,
    chapterId: number,
    body: { key: string; content_type: string; bytes: number; duration_ms: number; reader: string },
  ) => api.post<AudioRecording>(`/works/${workId}/chapters/${chapterId}/recordings`, body),

  updateRecording: (id: number, patch: { reader?: string; position?: number }) =>
    api.patch<AudioRecording>(`/audio/rec/${id}`, patch),

  deleteRecording: (id: number) => api.delete<void>(`/audio/rec/${id}`),
};

// Посещаемость и здоровье — только администратору.
export const statsApi = {
  traffic: (days: number) =>
    api.get<StatsTrafficRow[]>('/admin/stats/traffic', { params: { days } }),
  top: (kind: StatsTopKind, days: number) =>
    api.get<StatsTopRow[]>('/admin/stats/top', { params: { kind, days, limit: 50 } }),
  referrers: (days: number) =>
    api.get<StatsCountRow[]>('/admin/stats/referrers', { params: { days } }),
  searches: (days: number) => api.get<StatsSearches>('/admin/stats/searches', { params: { days } }),
  crawlers: (days: number) =>
    api.get<StatsCrawlerRow[]>('/admin/stats/crawlers', { params: { days } }),
  devices: (days: number) =>
    api.get<StatsDeviceRow[]>('/admin/stats/devices', { params: { days } }),
  health: () => api.get<StatsHealth>('/admin/stats/health'),
};

export default api;
