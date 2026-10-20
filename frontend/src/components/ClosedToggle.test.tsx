import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ClosedToggle } from './ClosedToggle';

describe('ClosedToggle', () => {
  afterEach(() => vi.restoreAllMocks());

  it('«Закрыть» зовёт onToggle(true) после подтверждения', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ClosedToggle closed={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(confirm).toHaveBeenCalledWith(
      'Закрытый том увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
    );
    expect(onToggle).toHaveBeenCalledWith(true);
  });

  it('не зовёт onToggle при отказе', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ClosedToggle closed={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('«Открыть» зовёт onToggle(false) без вопроса', async () => {
    const confirm = vi.spyOn(window, 'confirm');
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ClosedToggle closed onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    expect(confirm).not.toHaveBeenCalled();
    expect(onToggle).toHaveBeenCalledWith(false);
  });

  it.each([
    ['edition', 'закрыт вместе с изданием'],
    ['journal', 'закрыт вместе с журналом'],
    ['parent', 'закрыт вместе с томом'],
  ])('при closedBy=%s кнопка выключена с подписью', (closedBy, note) => {
    render(<ClosedToggle closed closedBy={closedBy} onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Открыть' })).toBeDisabled();
    expect(screen.getByText(note)).toBeInTheDocument();
  });

  it('closedBy=work оставляет кнопку рабочей', () => {
    render(<ClosedToggle closed closedBy="work" onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Открыть' })).toBeEnabled();
  });
});
