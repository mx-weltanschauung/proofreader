import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { marginNotesApi } from '../services/api';
import type { MarginNote } from '../types';
import { useAuth } from '../hooks/useAuth';
import { MyNotes } from './MyNotes';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function note(over: Partial<MarginNote>): MarginNote {
  return {
    id: 1,
    work_id: 7,
    target: 'work',
    chapter_id: null,
    chapter_title: '',
    start_page: null,
    end_page: null,
    page_missing: false,
    snapshot_start_page: null,
    quote_key: '',
    quote_tail: '',
    quote_text: '',
    body: 'тело',
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
    work_title: 'Том седьмой',
    work_slug: 'tom-7',
    context_chapter: '',
    ...over,
  };
}

function login() {
  useAuth.setState({
    user: { id: 1, email: '', nickname: 'читатель', role: 'reader' },
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/mine/notes']}>
      <MyNotes />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
});

describe('MyNotes', () => {
  it('зовёт записаться, если читатель не вошёл', async () => {
    const spy = vi.spyOn(marginNotesApi, 'mine');
    renderPage();
    const link = await screen.findByRole('link', { name: /записаться/i });
    expect(link).toHaveAttribute('href', '/join?next=%2Fmine%2Fnotes');
    expect(spy).not.toHaveBeenCalled();
  });

  it('три вида строк ведут на цитату, главу и том', async () => {
    login();
    vi.spyOn(marginNotesApi, 'mine').mockReturnValue(
      ok({
        total: 3,
        items: [
          note({
            id: 1,
            target: 'quote',
            chapter_id: 5,
            chapter_title: 'Глава пятая',
            context_chapter: 'Глава пятая',
            start_page: 12,
            end_page: 13,
            snapshot_start_page: 12,
            quote_key: 'начало цитаты',
            quote_tail: 'конец цитаты',
            quote_text: 'начало цитаты и конец цитаты',
            body: 'заметка к цитате',
          }),
          note({
            id: 2,
            target: 'chapter',
            chapter_id: 5,
            chapter_title: 'Глава пятая',
            body: 'заметка к главе',
          }),
          note({ id: 3, target: 'work', body: 'заметка к тому' }),
        ],
      }) as never,
    );
    renderPage();

    expect(await screen.findByText('заметка к цитате')).toBeInTheDocument();
    expect(screen.getByText('начало цитаты и конец цитаты')).toBeInTheDocument();
    const hrefs = screen.getAllByRole('link').map((a) => a.getAttribute('href') ?? '');
    const href = hrefs.find((h) => h.includes('quote=')) ?? '';
    expect(href).toContain('/works/7-tom-7/pages/12');
    expect(href).toContain(`?quote=${encodeURIComponent('начало цитаты')}`);
    expect(href).toContain(`&to=${encodeURIComponent('конец цитаты')}`);

    expect(hrefs).toContain('/works/7-tom-7/chapters/5');
    expect(hrefs).toContain('/works/7-tom-7');
  });

  // Номер в адресе — внутренний page_number; читателю печатается колонцифра.
  it('страница цитаты — печатной колонцифрой, с учётом сдвига и римского счёта', async () => {
    login();
    vi.spyOn(marginNotesApi, 'mine').mockReturnValue(
      ok({
        total: 2,
        items: [
          note({
            id: 1,
            target: 'quote',
            start_page: 12,
            end_page: 12,
            snapshot_start_page: 12,
            quote_key: 'ключ',
            body: 'со сдвигом',
            page_offset: 4,
            numbering_style: 'arabic',
          }),
          note({
            id: 2,
            work_id: 8,
            work_title: 'Передние листы',
            target: 'quote',
            start_page: 3,
            end_page: 3,
            snapshot_start_page: 3,
            quote_key: 'ключ',
            body: 'римская',
            page_offset: 1,
            numbering_style: 'roman',
          }),
        ],
      }) as never,
    );
    renderPage();
    await screen.findByText('со сдвигом');
    expect(screen.getByText('Том седьмой · с. 16')).toBeInTheDocument();
    expect(screen.getByText('Передние листы · с. IV')).toBeInTheDocument();
    // Адрес по-прежнему по внутреннему номеру.
    const hrefs = screen.getAllByRole('link').map((a) => a.getAttribute('href') ?? '');
    expect(hrefs.some((h) => h.includes('/pages/12?'))).toBe(true);
  });

  it('удалённая глава: пометка и ссылка на том', async () => {
    login();
    vi.spyOn(marginNotesApi, 'mine').mockReturnValue(
      ok({
        total: 1,
        items: [
          note({
            target: 'chapter',
            chapter_id: null,
            chapter_title: 'Старая глава',
            body: 'осиротела',
          }),
        ],
      }) as never,
    );
    renderPage();
    expect(await screen.findByText(/глава не найдена: Старая глава/)).toBeInTheDocument();
    const hrefs = screen.getAllByRole('link').map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('/works/7-tom-7');
    expect(hrefs.some((h) => h?.includes('/chapters/'))).toBe(false);
  });

  it('фильтр по тому шлёт work_id', async () => {
    login();
    const spy = vi.spyOn(marginNotesApi, 'mine').mockReturnValue(
      ok({
        total: 2,
        items: [
          note({ id: 1, work_id: 7, work_title: 'Том седьмой', body: 'а' }),
          note({ id: 2, work_id: 8, work_title: 'Том восьмой', work_slug: 'tom-8', body: 'б' }),
        ],
      }) as never,
    );
    renderPage();
    await screen.findByText('а');
    await userEvent.selectOptions(screen.getByRole('combobox', { name: /том/i }), '8');
    await waitFor(() => expect(spy).toHaveBeenLastCalledWith({ work_id: 8, limit: 50, offset: 0 }));
  });

  it('«Показать ещё» шлёт offset=50', async () => {
    login();
    const first = Array.from({ length: 50 }, (_, i) => note({ id: i + 1, body: `н${i + 1}` }));
    const spy = vi
      .spyOn(marginNotesApi, 'mine')
      .mockReturnValueOnce(ok({ total: 51, items: first }) as never)
      .mockReturnValueOnce(
        ok({ total: 51, items: [note({ id: 99, body: 'последняя' })] }) as never,
      );
    renderPage();
    await screen.findByText('н1');
    await userEvent.click(screen.getByRole('button', { name: /показать ещё/i }));
    expect(await screen.findByText('последняя')).toBeInTheDocument();
    expect(spy).toHaveBeenLastCalledWith({ work_id: undefined, limit: 50, offset: 50 });
    expect(screen.queryByRole('button', { name: /показать ещё/i })).toBeNull();
  });
});
