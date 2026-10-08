import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import toast from 'react-hot-toast';
import { CopyMcpUrl } from './CopyMcpUrl';

vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
});

describe('CopyMcpUrl', () => {
  it('кладёт в буфер адрес сервера', async () => {
    render(<CopyMcpUrl url="https://lib.example.org/mcp" />);
    await userEvent.click(screen.getByRole('button', { name: 'Скопировать' }));
    expect(writeText).toHaveBeenCalledWith('https://lib.example.org/mcp');
    expect(toast.success).toHaveBeenCalled();
  });

  it('говорит об отказе буфера', async () => {
    writeText.mockRejectedValueOnce(new Error('denied'));
    render(<CopyMcpUrl url="https://lib.example.org/mcp" />);
    await userEvent.click(screen.getByRole('button', { name: 'Скопировать' }));
    expect(toast.error).toHaveBeenCalledWith('Не удалось скопировать');
  });
});
