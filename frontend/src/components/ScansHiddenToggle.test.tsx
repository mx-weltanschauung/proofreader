import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useAuth } from '../hooks/useAuth';
import type { User } from '../types';
import { ScansHiddenToggle } from './ScansHiddenToggle';

const asRole = (role: User['role']) =>
  useAuth.setState({
    user: { id: 1, role } as User,
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });

describe('ScansHiddenToggle', () => {
  beforeEach(() => asRole('administrator'));
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('доступен только администратору', () => {
    asRole('editor');
    render(<ScansHiddenToggle kind="work" hidden={false} onToggle={vi.fn()} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('перед скрытием предупреждает о выданных ссылках и требует причину', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.spyOn(window, 'prompt').mockReturnValue('  письмо  ');
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ScansHiddenToggle kind="work" hidden={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Скрыть сканы' }));
    expect(confirm.mock.calls[0][0]).toMatch(/выданные ссылки|скачанные файлы/i);
    expect(onToggle).toHaveBeenCalledWith(true, 'письмо');
  });

  it('не отправляет действие без причины', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.spyOn(window, 'prompt').mockReturnValue('  ');
    const onToggle = vi.fn();
    render(<ScansHiddenToggle kind="chapter" hidden={false} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Скрыть сканы' }));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('при наследовании показывает источник и запрещает открытие здесь', () => {
    render(<ScansHiddenToggle kind="work" hidden hiddenBy="edition" onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Открыть сканы' })).toBeDisabled();
    expect(screen.getByText(/издания/)).toBeInTheDocument();
  });

  it('главе объясняет скрытие сканов всего тома', () => {
    render(<ScansHiddenToggle kind="chapter" hidden={false} hiddenBy="work" onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Открыть сканы' })).toBeDisabled();
    expect(screen.getByText(/у тома/)).toBeInTheDocument();
  });

  it('даёт снять собственный флаг главы при одновременно скрытом томе', async () => {
    vi.spyOn(window, 'prompt').mockReturnValue('открыть свой флаг');
    const onToggle = vi.fn().mockResolvedValue(undefined);
    render(<ScansHiddenToggle kind="chapter" hidden hiddenBy="chapter" onToggle={onToggle} />);
    await userEvent.click(screen.getByRole('button', { name: 'Открыть сканы' }));
    expect(onToggle).toHaveBeenCalledWith(false, 'открыть свой флаг');
  });
});
