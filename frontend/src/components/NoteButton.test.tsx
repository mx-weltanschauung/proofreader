import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { NoteButton } from './NoteButton';
import { marginNotesApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { quoteKey, quoteTail } from '../utils/quoteKey';
import type { MarginNote } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const NOTE: MarginNote = {
  id: 7,
  work_id: 19,
  target: 'quote',
  chapter_id: null,
  chapter_title: '',
  start_page: 5,
  end_page: 6,
  page_missing: false,
  snapshot_start_page: 5,
  quote_key: 'пятой',
  quote_tail: 'шестой',
  quote_text: 'пятой полосы. Текст шестой',
  body: 'мысль',
  created_at: '2026-10-29T00:00:00Z',
  updated_at: '2026-10-29T00:00:00Z',
};

/** Две полосы, как их рисует ReadingSurface: секции с data-page. */
function surface(): HTMLElement {
  const root = document.createElement('div');
  const five = document.createElement('div');
  five.setAttribute('data-page', '5');
  five.innerHTML = '<div class="page-html-content"><p>Текст пятой полосы.</p></div>';
  const six = document.createElement('div');
  six.setAttribute('data-page', '6');
  six.innerHTML = '<div class="page-html-content"><p>Текст шестой полосы.</p></div>';
  root.append(five, six);
  document.body.append(root);
  return root;
}

/** Выделение от «пятой» на полосе 5 до «шестой» на полосе 6. */
function selectAcrossSeam(root: HTMLElement): void {
  const fiveText = root.querySelector('[data-page="5"] p')!.firstChild!;
  const sixText = root.querySelector('[data-page="6"] p')!.firstChild!;
  const range = document.createRange();
  range.setStart(fiveText, 'Текст '.length);
  range.setEnd(sixText, 'Текст шестой'.length);
  const sel = window.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  act(() => {
    document.dispatchEvent(new Event('selectionchange'));
  });
}

function asReader() {
  useAuth.setState({
    user: { id: 3, role: 'reader', nickname: 'читатель' } as never,
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });
}

function renderButton(root: HTMLElement, onCreated = vi.fn()) {
  render(
    <MemoryRouter initialEntries={['/works/19/chapters/4']}>
      <NoteButton contentRef={{ current: root }} workId={19} onCreated={onCreated} />
    </MemoryRouter>,
  );
  return onCreated;
}

describe('NoteButton', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    window.getSelection()?.removeAllRanges();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('анониму кнопки нет', () => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    const root = surface();
    renderButton(root);
    expect(screen.queryByRole('button', { name: 'Заметка' })).toBeNull();
  });

  it('без выделения кнопка видна, но не нажимается', () => {
    asReader();
    const root = surface();
    renderButton(root);
    expect(screen.getByRole('button', { name: 'Заметка' })).toBeDisabled();
  });

  it('выделение на полосах 5—6 уходит якорями той же кнопки «Цитировать»', async () => {
    asReader();
    const create = vi.spyOn(marginNotesApi, 'create').mockImplementation(() => ok(NOTE));
    const root = surface();
    const onCreated = renderButton(root);

    selectAcrossSeam(root);
    const button = screen.getByRole('button', { name: 'Заметка' });
    expect(button).toBeEnabled();
    fireEvent.click(button);

    const field = await screen.findByRole('textbox', { name: 'Текст заметки' });
    fireEvent.change(field, { target: { value: 'мысль' } });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(create).toHaveBeenCalledWith(19, {
      target: 'quote',
      start_page: 5,
      end_page: 6,
      quote_key: quoteKey('пятой полосы.', 'Текст пятой полосы.'),
      quote_tail: quoteTail('Текст шестой', 'Текст шестой полосы.'),
      quote_text: expect.stringContaining('пятой полосы.'),
      body: 'мысль',
    });
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(NOTE));
    expect(screen.queryByRole('textbox', { name: 'Текст заметки' })).toBeNull();
  });

  // Поле получает фокус раньше эффектов компонента. Окно, ещё не поставленное
  // к выделению (top: 0 из CSS), утягивало страницу в начало документа —
  // замерено живым Chrome на полосе 20 из 60. Верх обязан стоять уже в
  // момент фокуса.
  it('поле стоит у выделения уже в момент фокуса', async () => {
    asReader();
    const root = surface();
    renderButton(root);
    vi.spyOn(window, 'scrollY', 'get').mockReturnValue(5000);
    let topAtFocus: string | null = null;
    const focus = HTMLTextAreaElement.prototype.focus;
    vi.spyOn(HTMLTextAreaElement.prototype, 'focus').mockImplementation(function (
      this: HTMLTextAreaElement,
      ...args
    ) {
      topAtFocus = (this.closest('.margin-note-popover') as HTMLElement | null)?.style.top ?? null;
      return focus.apply(this, args);
    });

    selectAcrossSeam(root);
    fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
    await screen.findByRole('textbox', { name: 'Текст заметки' });

    expect(topAtFocus).toBe('5008px');
  });

  it('длинная цитата обрезается до 1 000 знаков с многоточием', async () => {
    asReader();
    const create = vi.spyOn(marginNotesApi, 'create').mockImplementation(() => ok(NOTE));
    const root = surface();
    root.querySelector('[data-page="5"] p')!.textContent = 'слово '.repeat(400);
    renderButton(root);

    const text = root.querySelector('[data-page="5"] p')!.firstChild!;
    const range = document.createRange();
    range.setStart(text, 0);
    range.setEnd(text, (text.textContent ?? '').length);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);
    act(() => {
      document.dispatchEvent(new Event('selectionchange'));
    });

    fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
    fireEvent.change(await screen.findByRole('textbox', { name: 'Текст заметки' }), {
      target: { value: 'мысль' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    const sent = create.mock.calls[0][1] as { quote_text: string };
    expect(Array.from(sent.quote_text)).toHaveLength(1000);
    expect(sent.quote_text.endsWith('…')).toBe(true);
  });

  it('ответ 401 оставляет текст в поле и зовёт войти', async () => {
    asReader();
    vi.spyOn(marginNotesApi, 'create').mockImplementation(() =>
      Promise.reject({ response: { status: 401, data: { message: 'нужен вход' } } }),
    );
    const root = surface();
    const onCreated = renderButton(root);

    selectAcrossSeam(root);
    fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
    const field = await screen.findByRole('textbox', { name: 'Текст заметки' });
    fireEvent.change(field, { target: { value: 'не потерять' } });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    const hint = await screen.findByText(/Войдите, чтобы сохранить заметку/);
    expect(hint.closest('a') ?? hint.querySelector('a')).toHaveAttribute(
      'href',
      expect.stringContaining('/join'),
    );
    expect(screen.getByRole('textbox', { name: 'Текст заметки' })).toHaveValue('не потерять');
    expect(onCreated).not.toHaveBeenCalled();
  });
});
