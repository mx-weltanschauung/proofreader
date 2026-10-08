import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
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

  it('offers all four formats', async () => {
    await open();
    const menu = screen.getByRole('menu');
    for (const name of [/EPUB/, /FB2/, /Markdown/, /HTML/]) {
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
  });

  // Скачивание идёт обычной ссылкой: браузер сам покажет прогресс и возьмёт
  // имя файла из Content-Disposition.
  it('downloads through a plain link, not a script', async () => {
    await open();
    expect(screen.getByRole('menuitem', { name: /EPUB/ })).toHaveAttribute('download');
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
    it('с audio даёт пятый пункт — плейлист .m3u', async () => {
      render(<DownloadMenu href="/api/works/4/download" audio />);
      await userEvent.click(screen.getByRole('button', { name: /Скачать/ }));
      const m3u = screen.getByRole('menuitem', { name: /Аудио — плейлист \.m3u/ });
      expect(m3u).toHaveAttribute('href', '/api/works/4/download?format=m3u');
      expect(m3u).toHaveTextContent('для VLC и других плееров');
      expect(screen.getAllByRole('menuitem')).toHaveLength(5);
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
});
