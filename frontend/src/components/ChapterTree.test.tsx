import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter, User, Work } from '../types';
import { chaptersApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { ChapterTree } from './ChapterTree';

const WORK = { id: 3, title: 'Том 3', page_offset: 0 } as Work;

function chapter(id: number, extra: Partial<Chapter> = {}): Chapter {
  return {
    id,
    work_id: 3,
    title: `Глава ${id}`,
    type: 'chapter',
    order_number: id,
    start_page: id * 10,
    end_page: id * 10 + 9,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    ...extra,
  };
}

function renderTree(chapters: Chapter[], onToggleClosed = vi.fn().mockResolvedValue(undefined)) {
  render(
    <MemoryRouter>
      <ChapterTree
        chapters={chapters}
        work={WORK}
        canEdit
        onMove={vi.fn()}
        onDelete={vi.fn()}
        onToggleClosed={onToggleClosed}
      />
    </MemoryRouter>,
  );
  return onToggleClosed;
}

const signIn = (role: User['role']) =>
  useAuth.setState({
    user: { id: 1, role } as User,
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });

describe('ChapterTree: закрытые главы', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('закрытая строка приглушена и помечена', () => {
    signIn('editor');
    renderTree([chapter(1), chapter(2, { closed: true, closed_by: 'chapter' })]);
    const closedRow = screen.getByText('Глава 2').closest('.chapter-tree-node') as HTMLElement;
    expect(closedRow).toHaveClass('chapter-tree-closed');
    expect(within(closedRow).getByText('закрыт')).toBeInTheDocument();
    const openRow = screen.getByText('Глава 1').closest('.chapter-tree-node') as HTMLElement;
    expect(openRow).not.toHaveClass('chapter-tree-closed');
    expect(within(openRow).queryByText('закрыт')).toBeNull();
  });

  it('редактору кнопки «Закрыть» нет', () => {
    signIn('editor');
    renderTree([chapter(1)]);
    expect(screen.queryByRole('button', { name: 'Закрыть' })).toBeNull();
  });

  it('администратор закрывает главу: план, причина, колбэк', async () => {
    signIn('administrator');
    vi.spyOn(chaptersApi, 'closedPlan').mockResolvedValue({
      data: { chapters: 1, pages: 10, shared: [{ page_number: 19, neighbors: ['Сосед'] }] },
    } as never);
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.spyOn(window, 'prompt').mockReturnValue('жалоба');
    const onToggle = renderTree([chapter(1)]);
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(chaptersApi.closedPlan).toHaveBeenCalledWith(3, 1);
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('Сосед'));
    expect(onToggle).toHaveBeenCalledWith(1, true, 'жалоба');
  });
});
