import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DownloadMenu } from './DownloadMenu';

const open = async () => {
  const user = userEvent.setup();
  render(<DownloadMenu href="/api/works/47/download" />);
  await user.click(screen.getByRole('button', { name: 'Скачать' }));
  return user;
};

describe('DownloadMenu', () => {
  beforeEach(() => localStorage.clear());

  it('is closed until pressed', () => {
    render(<DownloadMenu href="/api/works/47/download" />);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('offers all five formats', async () => {
    await open();
    const menu = screen.getByRole('menu');
    for (const name of [/EPUB/, /FB2/, /PDF/, /Markdown/, /HTML/]) {
      expect(screen.getByRole('menuitem', { name })).toBeInTheDocument();
    }
    expect(menu).toBeInTheDocument();
  });

  it('points each item at the format it names', async () => {
    await open();
    expect(screen.getByRole('menuitem', { name: /EPUB/ })).toHaveAttribute(
      'href',
      '/api/works/47/download?format=epub',
    );
    expect(screen.getByRole('menuitem', { name: /FB2/ })).toHaveAttribute(
      'href',
      '/api/works/47/download?format=fb2',
    );
    expect(screen.getByRole('menuitem', { name: /PDF/ })).toHaveAttribute(
      'href',
      '/api/works/47/download?format=pdf',
    );
  });

  // Скачивание идёт обычной ссылкой: браузер сам покажет прогресс и возьмёт
  // имя файла из Content-Disposition.
  it('downloads through a plain link, not a script', async () => {
    await open();
    expect(screen.getByRole('menuitem', { name: /EPUB/ })).toHaveAttribute('download');
  });

  it('shows progress while preparing PDF, then saves it with the server filename', async () => {
    let finishRequest!: (response: Response) => void;
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          finishRequest = resolve;
        }),
    );
    const revokeUrl = vi.fn();
    Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:pdf'), revokeObjectURL: revokeUrl });
    const saved: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      saved.push(this.download);
    });

    try {
      const user = await open();
      await user.click(screen.getByRole('menuitem', { name: /PDF/ }));

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/works/47/download?format=pdf',
        expect.anything(),
      );
      expect(screen.getByRole('status')).toHaveTextContent('Готовим PDF');
      expect(screen.getByRole('button', { name: 'Скачать' })).toBeDisabled();
      expect(saved).toEqual([]);

      await act(async () => {
        finishRequest(
          new Response('%PDF', {
            headers: { 'Content-Disposition': 'attachment; filename="volume-47.pdf"' },
          }),
        );
      });
      await vi.waitFor(() => expect(saved).toEqual(['volume-47.pdf']));
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Скачать' })).toBeEnabled();
      // Дожидаемся отложенной очистки URL до следующего теста.
      await vi.waitFor(() => expect(revokeUrl).toHaveBeenCalledWith('blob:pdf'), {
        timeout: 3000,
      });
    } finally {
      vi.restoreAllMocks();
    }
  });

  it('clears PDF progress and reports a server error', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('Не удалось', { status: 500 }));
    try {
      const user = await open();
      await user.click(screen.getByRole('menuitem', { name: /PDF/ }));

      expect(fetchMock).toHaveBeenCalledOnce();
      expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось скачать');
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Скачать' })).toBeEnabled();
    } finally {
      vi.restoreAllMocks();
    }
  });

  it('explains what each format is for', async () => {
    await open();
    expect(screen.getByRole('menuitem', { name: /EPUB/ })).toHaveTextContent(/читалк/i);
  });

  it('marks the format remembered from a previous visit', async () => {
    localStorage.setItem('download-format', 'fb2');
    await open();
    expect(screen.getByRole('menuitem', { name: /FB2/ })).toHaveAttribute('aria-current', 'true');
    expect(screen.getByRole('menuitem', { name: /EPUB/ })).not.toHaveAttribute('aria-current');
  });

  it('marks the newly chosen format without waiting for a reopen', async () => {
    const user = await open();
    await user.click(screen.getByRole('menuitem', { name: /FB2/ }));

    await user.click(screen.getByRole('button', { name: 'Скачать' }));
    expect(screen.getByRole('menuitem', { name: /FB2/ })).toHaveAttribute('aria-current', 'true');
  });

  it('marks nothing on a first visit with no remembered format', async () => {
    await open();
    const menu = screen.getByRole('menu');
    expect(menu.querySelector('[aria-current]')).not.toBeInTheDocument();
  });

  it('closes on Escape', async () => {
    const user = await open();
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('reports its open state on the toggle', async () => {
    const user = await open();
    expect(screen.getByRole('button', { name: 'Скачать' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    await user.keyboard('{Escape}');
    expect(screen.getByRole('button', { name: 'Скачать' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  describe('звук', () => {
    it('с audio даёт шестой пункт — плейлист .m3u', async () => {
      render(<DownloadMenu href="/api/works/4/download" audio />);
      await userEvent.click(screen.getByRole('button', { name: /Скачать/ }));
      const m3u = screen.getByRole('menuitem', { name: /Аудио — плейлист \.m3u/ });
      expect(m3u).toHaveAttribute('href', '/api/works/4/download?format=m3u');
      expect(m3u).toHaveTextContent('для VLC и других плееров');
      expect(screen.getAllByRole('menuitem')).toHaveLength(6);
    });

    // Спека: запоминание формата на плейлист не распространяется — иначе
    // следующий клик «Скачать» подсвечивал бы звук вместо книги.
    it('клик по плейлисту не запоминается как формат', async () => {
      localStorage.setItem('download-format', 'epub');
      render(<DownloadMenu href="/api/works/4/download" audio />);
      await userEvent.click(screen.getByRole('button', { name: /Скачать/ }));
      await userEvent.click(screen.getByRole('menuitem', { name: /Аудио/ }));
      expect(localStorage.getItem('download-format')).toBe('epub');
    });

    it('без audio пункта нет', async () => {
      render(<DownloadMenu href="/api/works/4/download" />);
      await userEvent.click(screen.getByRole('button', { name: /Скачать/ }));
      expect(screen.queryByRole('menuitem', { name: /Аудио/ })).toBeNull();
    });
  });

  describe('withToken', () => {
    afterEach(() => vi.restoreAllMocks());

    it('качает через fetch с Authorization и сохраняет под именем из заголовка', async () => {
      localStorage.setItem('token', 'tok');
      const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response('книга', {
          status: 200,
          headers: { 'Content-Disposition': 'attachment; filename="tom.epub"' },
        }),
      );
      const createUrl = vi.fn(() => 'blob:x');
      const revokeUrl = vi.fn();
      Object.assign(URL, { createObjectURL: createUrl, revokeObjectURL: revokeUrl });
      const saved: string[] = [];
      const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
        this: HTMLAnchorElement,
      ) {
        saved.push(this.download);
      });

      const user = userEvent.setup();
      render(<DownloadMenu href="/api/works/47/download" withToken />);
      await user.click(screen.getByRole('button', { name: 'Скачать' }));
      await user.click(screen.getByRole('menuitem', { name: /EPUB/ }));

      await vi.waitFor(() => expect(click).toHaveBeenCalled());
      const [url, init] = fetchMock.mock.calls[0];
      expect(String(url)).toContain('/api/works/47/download?format=epub');
      expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer tok');
      expect(saved).toEqual(['tom.epub']);
      expect(revokeUrl).not.toHaveBeenCalled();
      await vi.waitFor(() => expect(revokeUrl).toHaveBeenCalledWith('blob:x'), { timeout: 3000 });
    });

    it('на отказ сервера не сохраняет тело ошибки, а говорит об ошибке', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('Не найдено', { status: 404 }));
      const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
      const user = userEvent.setup();
      render(<DownloadMenu href="/api/works/47/download" withToken />);
      await user.click(screen.getByRole('button', { name: 'Скачать' }));
      await user.click(screen.getByRole('menuitem', { name: /EPUB/ }));
      expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось скачать');
      expect(click).not.toHaveBeenCalled();
    });
  });
});
