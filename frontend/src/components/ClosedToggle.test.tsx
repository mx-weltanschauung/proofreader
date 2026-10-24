import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ClosedToggle } from './ClosedToggle';
import { useAuth } from '../hooks/useAuth';
import type { User } from '../types';

const asRole = (role: User['role']) =>
  useAuth.setState({
    user: { id: 1, role } as User,
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });

const PROMPT = 'Причина (обязательно): письмо, от кого, о чём';

describe('ClosedToggle', () => {
  beforeEach(() => asRole('administrator'));
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('редактору кнопки нет', () => {
    asRole('editor');
    render(<ClosedToggle closed={false} onToggle={vi.fn()} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('«Закрыть» после подтверждения спрашивает причину и отдаёт её вторым аргументом', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const prompt = vi.spyOn(window, 'prompt').mockReturnValue('  письмо издателя ');
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ClosedToggle closed={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(confirm).toHaveBeenCalledWith(
      'Закрытый том увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
    );
    expect(prompt).toHaveBeenCalledWith(PROMPT);
    expect(onToggle).toHaveBeenCalledWith(true, 'письмо издателя');
  });

  it.each([
    [
      'edition',
      'Закрытое собрание увидят только редакторы. Если оно есть в выложенном статическом архиве, пересоберите и выложите архив.',
    ],
    [
      'journal',
      'Закрытый журнал увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
    ],
  ] as const)('для %s спрашивает своим текстом', async (kind, text) => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(<ClosedToggle closed={false} kind={kind} onToggle={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(confirm).toHaveBeenCalledWith(text);
  });

  it('отказ в подтверждении не зовёт onToggle и не спрашивает причину', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    const prompt = vi.spyOn(window, 'prompt');
    const onToggle = vi.fn();
    render(<ClosedToggle closed={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(prompt).not.toHaveBeenCalled();
    expect(onToggle).not.toHaveBeenCalled();
  });

  it.each([[null], [''], ['   ']])('причина %j — ничего не делаем', async (answer) => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.spyOn(window, 'prompt').mockReturnValue(answer);
    const onToggle = vi.fn();
    render(<ClosedToggle closed={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('«Открыть» без подтверждения, но с причиной', async () => {
    const confirm = vi.spyOn(window, 'confirm');
    vi.spyOn(window, 'prompt').mockReturnValue('ошибочно закрыли');
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ClosedToggle closed onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    expect(confirm).not.toHaveBeenCalled();
    expect(onToggle).toHaveBeenCalledWith(false, 'ошибочно закрыли');
  });

  it('«Открыть» с отменой причины ничего не делает', async () => {
    vi.spyOn(window, 'prompt').mockReturnValue(null);
    const onToggle = vi.fn();
    render(<ClosedToggle closed onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }));
    expect(onToggle).not.toHaveBeenCalled();
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

  describe('глава', () => {
    const plan = {
      chapters: 3,
      pages: 40,
      shared: [
        { page_number: 12, neighbors: ['Глава А'] },
        { page_number: 20, neighbors: ['Глава А', 'Глава Б'] },
      ],
    };

    it('показывает план с соседями до вопроса о причине', async () => {
      const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
      const prompt = vi.spyOn(window, 'prompt').mockReturnValue('жалоба');
      const onToggle = vi.fn().mockResolvedValue(undefined);
      const loadPlan = vi.fn().mockResolvedValue(plan);
      render(
        <ClosedToggle kind="chapter" closed={false} onToggle={onToggle} loadPlan={loadPlan} />,
      );
      await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
      expect(confirm).toHaveBeenCalledWith(
        'Закроются: глав — 3, полос — 40. На общих полосах потеряют кусок: Глава А, Глава Б.',
      );
      expect(prompt).toHaveBeenCalled();
      expect(onToggle).toHaveBeenCalledWith(true, 'жалоба');
    });

    it('без общих полос вторую фразу не печатает', async () => {
      const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
      const loadPlan = vi.fn().mockResolvedValue({ chapters: 1, pages: 5, shared: [] });
      render(<ClosedToggle kind="chapter" closed={false} onToggle={vi.fn()} loadPlan={loadPlan} />);
      await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
      expect(confirm).toHaveBeenCalledWith('Закроются: глав — 1, полос — 5.');
    });

    it('closedBy=parent — кнопка выключена, подпись «закрыта вместе с главой выше»', () => {
      render(<ClosedToggle kind="chapter" closed closedBy="parent" onToggle={vi.fn()} />);
      expect(screen.getByRole('button', { name: 'Открыть' })).toBeDisabled();
      expect(screen.getByText('закрыта вместе с главой выше')).toBeInTheDocument();
    });
  });
});
