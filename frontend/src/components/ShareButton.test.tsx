import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import toast from 'react-hot-toast';
import { ShareButton } from './ShareButton';

vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

const title = 'Том 6 — Ленин В. И.';
const path = '/works/49-lenin-t06';

let written: Record<string, string> = {};
let write: ReturnType<typeof vi.fn>;

function stubShare(share: unknown, canShare: unknown = vi.fn(() => true)) {
  Object.defineProperty(navigator, 'share', { configurable: true, value: share });
  Object.defineProperty(navigator, 'canShare', { configurable: true, value: canShare });
}

beforeEach(() => {
  written = {};
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  // jsdom не знает ни ClipboardItem, ни navigator.clipboard.write, ни
  // navigator.share — все три подделываются вручную, как в CiteButton.test.
  class ItemStub {
    constructor(public parts: Record<string, Blob>) {}
  }
  vi.stubGlobal('ClipboardItem', ItemStub);
  write = vi.fn(async (items: ItemStub[]) => {
    for (const [type, blob] of Object.entries(items[0].parts)) {
      written[type] = await blob.text();
    }
  });
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { write } });
  stubShare(undefined, undefined);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const click = () => userEvent.click(screen.getByRole('button', { name: 'Поделиться' }));

describe('ShareButton', () => {
  it('открывает меню системы с подписью и полным адресом', async () => {
    const share = vi.fn(async () => {});
    stubShare(share);
    render(<ShareButton title={title} path={path} />);
    await click();
    const url = `${window.location.origin}${path}`;
    expect(share).toHaveBeenCalledWith({ title, text: title, url });
    expect(write).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it('молчит, когда читатель закрыл меню системы', async () => {
    stubShare(vi.fn(async () => Promise.reject(new DOMException('cancel', 'AbortError'))));
    render(<ShareButton title={title} path={path} />);
    await click();
    expect(toast.error).not.toHaveBeenCalled();
    expect(write).not.toHaveBeenCalled();
  });

  it('говорит об отказе меню системы и не лезет в буфер после него', async () => {
    stubShare(vi.fn(async () => Promise.reject(new DOMException('no', 'NotAllowedError'))));
    render(<ShareButton title={title} path={path} />);
    await click();
    expect(toast.error).toHaveBeenCalledWith('Не удалось поделиться');
    expect(write).not.toHaveBeenCalled();
  });

  it('без меню системы кладёт в буфер подпись и адрес двумя гранями', async () => {
    render(<ShareButton title={title} path={path} />);
    await click();
    const url = `${window.location.origin}${path}`;
    expect(written['text/plain']).toBe(`${title}\n${url}\n`);
    expect(written['text/html']).toContain(`<a href="${url}">`);
    expect(written['text/html']).toContain('Том 6 — Ленин В. И.');
    expect(toast.success).toHaveBeenCalledWith('Ссылка скопирована');
  });

  it('уходит в буфер, когда меню системы есть, но адрес не берёт', async () => {
    const share = vi.fn(async () => {});
    stubShare(
      share,
      vi.fn(() => false),
    );
    render(<ShareButton title={title} path={path} />);
    await click();
    expect(share).not.toHaveBeenCalled();
    expect(written['text/plain']).toContain(path);
  });

  it('говорит об отказе буфера', async () => {
    write.mockRejectedValueOnce(new Error('denied'));
    render(<ShareButton title={title} path={path} />);
    await click();
    expect(toast.error).toHaveBeenCalledWith('Не удалось скопировать');
  });
});
