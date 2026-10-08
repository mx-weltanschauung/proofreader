import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { documentsApi } from '../services/api';
import type { Document } from '../types';
import { DocumentReviewQueue } from './DocumentReviewQueue';

vi.mock('../services/api', () => ({
  documentsApi: {
    review: vi.fn(),
    approve: vi.fn(),
    reject: vi.fn(),
  },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function doc(over: Partial<Document>): Document {
  return {
    id: 1,
    slug: 'pro-lenina',
    title: 'Про Ленина',
    markdown_content: 'текст',
    owner_id: 2,
    author_nickname: 'чтец',
    published_title: '',
    published_markdown: '',
    published_at: null,
    was_published: false,
    review_status: 'на_рассмотрении',
    submitted_at: '2026-09-18T00:00:00Z',
    created_at: '2026-09-18T00:00:00Z',
    updated_at: '2026-09-18T00:00:00Z',
    ...over,
  };
}

function renderQueue() {
  return render(
    <MemoryRouter>
      <DocumentReviewQueue />
    </MemoryRouter>,
  );
}

describe('DocumentReviewQueue', () => {
  beforeEach(() => {
    vi.mocked(documentsApi.review)
      .mockReset()
      .mockReturnValue(ok([doc({})]));
    vi.mocked(documentsApi.approve).mockReset();
    vi.mocked(documentsApi.reject).mockReset();
  });

  it('показывает заглавие, подпись автора и ссылку на предпросмотр', async () => {
    renderQueue();

    expect(await screen.findByText('Про Ленина')).toBeInTheDocument();
    expect(screen.getByText(/чтец/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /предпросмотр/i })).toHaveAttribute(
      'href',
      '/documents/чтец/pro-lenina',
    );
  });

  it('принимает разбор', async () => {
    vi.mocked(documentsApi.approve).mockReturnValue(ok(doc({ review_status: 'одобрено' })));
    renderQueue();

    await userEvent.click(await screen.findByRole('button', { name: /принять/i }));
    expect(vi.mocked(documentsApi.approve)).toHaveBeenCalledWith({
      nickname: 'чтец',
      slug: 'pro-lenina',
    });
  });

  it('не отклоняет без причины', async () => {
    vi.mocked(documentsApi.reject).mockReturnValue(
      ok(doc({ review_status: 'отклонено', reject_reason: 'не_по_теме' })),
    );
    renderQueue();

    await screen.findByText('Про Ленина');
    // Кнопка отказа неактивна, пока причина не выбрана: автору обязаны
    // назвать, за что.
    expect(screen.getByRole('button', { name: /отклонить/i })).toBeDisabled();
    await userEvent.selectOptions(screen.getByLabelText(/причина/i), 'не_по_теме');
    await userEvent.click(screen.getByRole('button', { name: /отклонить/i }));
    expect(vi.mocked(documentsApi.reject)).toHaveBeenCalledWith(
      { nickname: 'чтец', slug: 'pro-lenina' },
      'не_по_теме',
    );
  });

  it('показывает пустую очередь', async () => {
    vi.mocked(documentsApi.review).mockReturnValue(ok([]));
    renderQueue();

    expect(await screen.findByText(/очередь пуста/i)).toBeInTheDocument();
  });
});
