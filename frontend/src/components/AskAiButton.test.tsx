import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import toast from 'react-hot-toast';
import { AskAiButton } from './AskAiButton';
import { askAiPrompt } from '../utils/askAi';
import type { Chapter, Work } from '../types';

vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

const work = {
  id: 49,
  slug: 'lenin-t06',
  title: 'Том 6',
  author: '',
  page_offset: 0,
  numbering_style: 'arabic',
} as unknown as Work;
const chapter = {
  id: 10125,
  slug: 'chto-delat',
  title: 'Что делать?',
  start_page: 1,
  end_page: 192,
} as unknown as Chapter;

let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
});

describe('AskAiButton', () => {
  it('кладёт в буфер запрос со ссылкой на текст главы', async () => {
    render(<AskAiButton prompt={() => askAiPrompt(work, chapter, window.location.origin)} />);
    await userEvent.click(screen.getByRole('button', { name: 'Спросить нейросеть' }));
    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText.mock.calls[0][0]).toContain(
      `${window.location.origin}/works/49-lenin-t06/chapters/10125-chto-delat.md`,
    );
    expect(toast.success).toHaveBeenCalled();
  });

  it('говорит об отказе буфера', async () => {
    writeText.mockRejectedValueOnce(new Error('denied'));
    render(<AskAiButton prompt={() => askAiPrompt(work, chapter, window.location.origin)} />);
    await userEvent.click(screen.getByRole('button', { name: 'Спросить нейросеть' }));
    expect(toast.error).toHaveBeenCalledWith('Не удалось скопировать');
  });
});
