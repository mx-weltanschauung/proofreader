import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { documentsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import type { Document } from '../types';
import { DocumentForm } from './DocumentForm';

vi.mock('../services/api', () => ({
  documentsApi: {
    get: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    submit: vi.fn(),
    unpublish: vi.fn(),
  },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function doc(over: Partial<Document> = {}): Document {
  return {
    id: 7,
    slug: 'razbor-o-chernyshevskom',
    title: 'Разбор о Чернышевском',
    markdown_content: 'текст разбора',
    owner_id: 1,
    author_nickname: 'чтец',
    published_title: '',
    published_markdown: '',
    published_at: null,
    was_published: false,
    review_status: 'черновик',
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    ...over,
  };
}

function renderEdit() {
  // MarkdownEditor зовёт useReadingPrefs() — в приложении маршрут уже стоит
  // внутри ReadingPreferencesProvider (App.tsx), здесь оборачиваем локально,
  // как делает PageSuggest.test.tsx.
  return render(
    <ReadingPreferencesProvider>
      <MemoryRouter initialEntries={['/documents/чтец/razbor-o-chernyshevskom/edit']}>
        <Routes>
          <Route path="/documents/:nickname/:slug/edit" element={<DocumentForm />} />
        </Routes>
      </MemoryRouter>
    </ReadingPreferencesProvider>,
  );
}

describe('DocumentForm — состояние и отправка на проверку', () => {
  beforeEach(() => {
    vi.mocked(documentsApi.get).mockReset();
    vi.mocked(documentsApi.update).mockReset();
    vi.mocked(documentsApi.create).mockReset();
    vi.mocked(documentsApi.submit).mockReset();
    vi.mocked(documentsApi.unpublish).mockReset();
    useAuth.setState({
      user: { id: 1, email: '', role: 'reader', nickname: 'чтец' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
  });

  it('показывает автору, какая редакция на людях', async () => {
    vi.mocked(documentsApi.get).mockReturnValue(
      ok(doc({ published_at: '2026-09-18T00:00:00Z', review_status: 'на_рассмотрении' })),
    );
    renderEdit();

    expect(await screen.findByText(/на людях — прежняя редакция/i)).toBeInTheDocument();
  });

  it('называет причину отказа словами', async () => {
    vi.mocked(documentsApi.get).mockReturnValue(
      ok(doc({ review_status: 'отклонено', reject_reason: 'не_по_теме' })),
    );
    renderEdit();

    expect(await screen.findByText(/не по теме читальни/i)).toBeInTheDocument();
  });

  it('отправляет черновик на проверку', async () => {
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc()));
    vi.mocked(documentsApi.submit).mockReturnValue(ok(doc({ review_status: 'на_рассмотрении' })));
    renderEdit();

    await userEvent.click(await screen.findByRole('button', { name: /отправить на проверку/i }));

    expect(documentsApi.submit).toHaveBeenCalledWith({
      nickname: 'чтец',
      slug: 'razbor-o-chernyshevskom',
    });
  });

  it('показывает отказ по пределу частоты, а не запасную фразу', async () => {
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc()));
    vi.mocked(documentsApi.submit).mockRejectedValue({
      response: {
        status: 429,
        data: { message: 'С этого адреса сегодня уже отправляли разборы. Попробуйте завтра.' },
      },
    });
    renderEdit();

    await userEvent.click(await screen.findByRole('button', { name: /отправить на проверку/i }));

    expect(await screen.findByText(/уже отправляли разборы/i)).toBeInTheDocument();
  });

  it('снимает опубликованный разбор с публикации', async () => {
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc({ published_at: '2026-09-18T00:00:00Z' })));
    vi.mocked(documentsApi.unpublish).mockReturnValue(
      ok(doc({ published_at: null, was_published: true })),
    );
    renderEdit();

    await userEvent.click(await screen.findByRole('button', { name: /снять с публикации/i }));

    expect(documentsApi.unpublish).toHaveBeenCalledWith({
      nickname: 'чтец',
      slug: 'razbor-o-chernyshevskom',
    });
    expect(await screen.findByText(/снят с публикации/i)).toBeInTheDocument();
  });

  it('предупреждает, что правка снятого с очереди разбора вернула его в черновики', async () => {
    // Update() на сервере молча откатывает «на_рассмотрении» в «черновик»
    // при любой правке тела (internal/repository/document_repository.go) —
    // интерфейс обязан сказать об этом автору, а не промолчать.
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc({ review_status: 'на_рассмотрении' })));
    vi.mocked(documentsApi.update).mockReturnValue(ok(doc({ review_status: 'черновик' })));
    renderEdit();

    await screen.findByDisplayValue('Разбор о Чернышевском');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(
      await screen.findByText(/вернула разбор в черновики.*отправ.*снова/is),
    ).toBeInTheDocument();
  });

  // M10. Серверные пути к чужому разбору закрыты давно (mayEditDocument), но
  // набор предложенных кнопок был неверен: постороннему вошедшему показывали
  // и поле заглавия, и редактор, и подборщик вклеек, а отказ прилетал уже по
  // нажатии «Сохранить».
  it('постороннему вошедшему не показывает ни формы правки, ни подборщика вклеек', async () => {
    useAuth.setState({
      user: { id: 6, email: '', role: 'reader', nickname: 'посторонний' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc()));
    renderEdit();

    expect(await screen.findByText(/этот разбор собрал другой читатель/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/заглавие/i)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Сохранить' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Вклейка' })).toBeNull();
    expect(screen.queryByRole('button', { name: /отправить на проверку/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /снять с публикации/i })).toBeNull();
  });

  // Редактор читательский разбор тоже не правит — «мы размещаем, автор
  // собирает», то же разделение, что у подборок.
  it('редактору чужой читательский разбор править не предлагает', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@example.org', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc()));
    renderEdit();

    expect(await screen.findByText(/этот разбор собрал другой читатель/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/заглавие/i)).toBeNull();
  });

  // Контрольная группа: у автора форма на месте — отказ выше держится на
  // владельце, а не на том, что форму закрыли всем.
  it('автору форма правки по-прежнему открыта', async () => {
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc()));
    renderEdit();

    expect(await screen.findByDisplayValue('Разбор о Чернышевском')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Вклейка' })).toBeInTheDocument();
    expect(screen.queryByText(/этот разбор собрал другой читатель/i)).toBeNull();
  });

  it('гостю форма разбора предлагает записаться, а не редактировать', async () => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    vi.mocked(documentsApi.get).mockReturnValue(ok(doc()));
    renderEdit();

    expect(
      await screen.findByText(/собирать и публиковать свой разбор может только читатель/i),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/заглавие/i)).toBeNull();
  });
});
