import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { cleanup, render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AxiosError, type AxiosResponse } from 'axios';
import { NoteButton } from './NoteButton';
import api, { marginNotesApi } from '../services/api';
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
    // Сначала кнопка уходит (очистка RTL), и лишь потом сбрасывается вход:
    // иначе сброс перерисовывает смонтированную кнопку вне act.
    cleanup();
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

  // Экран потока уводит из чтения по Escape на window (WorkRead): Escape
  // в поле заметки и вне его до window не доходит.
  it('Escape закрывает пустое поле и не всплывает до window', async () => {
    asReader();
    const root = surface();
    renderButton(root);
    const onWindow = vi.fn();
    window.addEventListener('keydown', onWindow);
    try {
      selectAcrossSeam(root);
      fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
      const field = await screen.findByRole('textbox', { name: 'Текст заметки' });

      // Фокус ушёл в текст: Escape возвращает его в поле, поле открыто.
      fireEvent.keyDown(document.body, { key: 'Escape' });
      expect(onWindow).not.toHaveBeenCalled();
      expect(field).toHaveFocus();

      fireEvent.keyDown(field, { key: 'Escape' });
      expect(onWindow).not.toHaveBeenCalled();
      expect(screen.queryByRole('dialog', { name: 'Новая заметка' })).toBeNull();
    } finally {
      window.removeEventListener('keydown', onWindow);
    }
  });

  it('Escape с начатым текстом спрашивает, прежде чем закрыть', async () => {
    asReader();
    const root = surface();
    renderButton(root);
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
    selectAcrossSeam(root);
    fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
    const field = await screen.findByRole('textbox', { name: 'Текст заметки' });
    fireEvent.change(field, { target: { value: 'недописанная мысль' } });

    fireEvent.keyDown(field, { key: 'Escape' });
    expect(confirm).toHaveBeenCalledWith('Закрыть заметку без сохранения?');
    expect(field).toHaveValue('недописанная мысль');

    confirm.mockReturnValue(true);
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Новая заметка' })).toBeNull();
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

  // Отказ 401 настоящего экземпляра api — не подменой marginNotesApi: общий
  // перехватчик ответа на 401 переписывает window.location, и страница
  // уходила вместе с набранным. Запись заметки от этого отказывается.
  it('401 настоящего api не уводит со страницы, текст остаётся', async () => {
    asReader();
    const originalAdapter = api.defaults.adapter;
    const originalLocation = window.location;
    Object.defineProperty(window, 'location', {
      value: { pathname: '/works/19/chapters/4', search: '', href: '' },
      writable: true,
      configurable: true,
    });
    api.defaults.adapter = async (config) => {
      throw new AxiosError('Unauthorized', 'ERR_BAD_REQUEST', config, null, {
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config,
        data: { message: 'нужен вход' },
      });
    };
    try {
      const root = surface();
      const onCreated = renderButton(root);
      selectAcrossSeam(root);
      fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
      fireEvent.change(await screen.findByRole('textbox', { name: 'Текст заметки' }), {
        target: { value: 'не потерять' },
      });
      fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

      expect(await screen.findByText(/Войдите, чтобы сохранить заметку/)).toBeInTheDocument();
      expect(window.location.href).toBe('');
      expect(screen.getByRole('textbox', { name: 'Текст заметки' })).toHaveValue('не потерять');
      expect(onCreated).not.toHaveBeenCalled();
    } finally {
      api.defaults.adapter = originalAdapter;
      Object.defineProperty(window, 'location', {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  // В чтении подборки соседний элемент — другой том. Поле, открытое на
  // одном, пишет заметку туда, откуда снята цитата, а не в том, что стоит
  // на экране к моменту записи.
  it('заметка уходит в том, с которого снята цитата', async () => {
    asReader();
    const create = vi.spyOn(marginNotesApi, 'create').mockImplementation(() => ok(NOTE));
    const root = surface();
    const ref = { current: root };
    const ui = (workId: number) => (
      <MemoryRouter>
        <NoteButton contentRef={ref} workId={workId} onCreated={vi.fn()} />
      </MemoryRouter>
    );
    const { rerender } = render(ui(19));

    selectAcrossSeam(root);
    fireEvent.click(screen.getByRole('button', { name: 'Заметка' }));
    fireEvent.change(await screen.findByRole('textbox', { name: 'Текст заметки' }), {
      target: { value: 'мысль' },
    });
    rerender(ui(20));
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(create.mock.calls[0][0]).toBe(19);
  });
});
