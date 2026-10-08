import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import App from './App';
import { useAuth } from './hooks/useAuth';
import { documentsApi, suggestionsApi } from './services/api';

// Сторож адреса «/documents/review»: он обязан открывать очередь модерации,
// а не карточку разбора с id="review". На сервере (router.go) это была бы
// ловушка порядка подроутеров; на клиенте React Router v6 ранжирует маршруты
// по специфичности сам (см. комментарий над этими двумя <Route> в App.tsx) —
// статический сегмент побеждает «:id» независимо от того, какая строка ниже,
// поэтому перестановка строк этот тест не роняет (проверено руками). Тест
// всё равно нужен постоянным: паспорт ссылался на файл теста уровня
// приложения, которого не существовало, а рендерит он настоящий App.tsx, а
// не копию списка маршрутов, — и ловит регрессию, если сам путь когда-нибудь
// перестанет быть более специфичным (например, «/documents/review» станет
// параметром или обрастёт вложенностью, снижающей счёт).

vi.mock('./services/api', () => ({
  documentsApi: {
    review: vi.fn(),
    view: vi.fn(),
  },
  suggestionsApi: {
    queue: vi.fn(),
  },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

describe('App — маршрут /documents/review', () => {
  beforeEach(() => {
    vi.mocked(documentsApi.review).mockReset().mockReturnValue(ok([]));
    vi.mocked(documentsApi.view)
      .mockReset()
      .mockReturnValue(ok(undefined as never));
    vi.mocked(suggestionsApi.queue)
      .mockReset()
      .mockReturnValue(ok({ items: [], total: 0 }));

    // Роль редактора: пускает и в очередь модерации (RequireRole), и в
    // значок счётчика правок в шапке (canEdit) — обе ветки должны отработать
    // без падений, чтобы регрессия маршрута не тонула среди посторонних
    // ошибок.
    useAuth.setState({
      user: { id: 1, email: 'editor@example.org', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
  });

  it('открывает очередь разборов на «/documents/review», а не карточку разбора с id «review»', async () => {
    window.history.pushState({}, '', '/documents/review');

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Очередь разборов' })).toBeInTheDocument();
    // waitFor, а не голое expect: заголовок появляется в DOM раньше, чем
    // React успевает прогнать пассивный эффект, который и делает запрос.
    // findByRole ждёт DOM, а вызова не ждал никто — тест давал красное
    // примерно раз из пяти полных прогонов на неизменном дереве. Нестабильный
    // сторож перестаёт сторожить быстрее, чем зелёный по неверной причине:
    // первое же «да это дребезг, перезапусти» — и настоящая регрессия уедет
    // под тем же объяснением.
    await waitFor(() => {
      expect(vi.mocked(documentsApi.review)).toHaveBeenCalled();
    });
    // Регрессия выглядела бы так: DocumentView запросит
    // documentsApi.view(NaN) вместо documentsApi.review() — «review» ушёл в
    // :id.
    expect(vi.mocked(documentsApi.view)).not.toHaveBeenCalled();
    expect(screen.queryByText(/документ не найден/i)).toBeNull();
  });
});

// Тот же сторож, что выше, для «/documents/new»: статический сегмент «new»
// обязан выигрывать у «:slug» и открывать форму создания, а не карточку
// разбора со слагом "new" (DocumentView запросил бы
// documentsApi.view({ slug: 'new' })). Отдельный describe, а не второй `it`
// в блоке выше, — маршрут другой, и общий заголовок describe иначе вводит
// в заблуждение.
describe('App — маршрут /documents/new', () => {
  beforeEach(() => {
    vi.mocked(documentsApi.review).mockReset().mockReturnValue(ok([]));
    vi.mocked(documentsApi.view)
      .mockReset()
      .mockReturnValue(ok(undefined as never));
    vi.mocked(suggestionsApi.queue)
      .mockReset()
      .mockReturnValue(ok({ items: [], total: 0 }));

    useAuth.setState({
      user: { id: 1, email: 'reader@example.org', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
  });

  it('открывает форму нового документа на «/documents/new», а не карточку разбора со слагом «new»', async () => {
    window.history.pushState({}, '', '/documents/new');

    render(<App />);

    // Таймаут увеличен против дефолтного: цепочка импорта у DocumentForm
    // тяжелее, чем у DocumentReviewQueue (тянет CutPicker/MarkdownEditor), и
    // первая трансформация модулей в тестовой среде укладывается не всегда в
    // 1000 мс по умолчанию.
    expect(
      await screen.findByRole('heading', { name: 'Новый документ' }, { timeout: 5000 }),
    ).toBeInTheDocument();
    // Регрессия выглядела бы так: DocumentView запросит
    // documentsApi.view({ slug: 'new' }) вместо показа формы создания —
    // «new» ушёл в «:slug».
    expect(vi.mocked(documentsApi.view)).not.toHaveBeenCalled();
    expect(screen.queryByText(/документ не найден/i)).toBeNull();
  });
});
