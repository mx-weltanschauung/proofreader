import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter, Work } from '../types';
import { VolumeManagePanel } from './VolumeManagePanel';

const WORK = { id: 41, title: 'Том 16', file_path: 'works/41/original/t16.pdf' } as Work;
const CHAPTERS = [
  { id: 1, work_id: 41, title: 'Глава', start_page: 1, end_page: 2, order_number: 1 } as Chapter,
];

function setup(props: Partial<React.ComponentProps<typeof VolumeManagePanel>> = {}) {
  const handlers = {
    onUpload: vi.fn(),
    onCreatePages: vi.fn().mockResolvedValue(true),
    onDeleteWork: vi.fn(),
    onDeleteChapter: vi.fn(),
    onMoveChapter: vi.fn().mockResolvedValue(undefined),
    onReorderingChange: vi.fn(),
  };
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <VolumeManagePanel
        work={WORK}
        chapters={CHAPTERS}
        isUploading={false}
        isCreatingPages={false}
        reordering={false}
        {...handlers}
        {...props}
      />
    </MemoryRouter>,
  );
  return { user, ...handlers };
}

describe('VolumeManagePanel', () => {
  it('свёрнута по умолчанию', () => {
    // jsdom не реализует UA-стиль `details:not([open]) > *:not(summary) {
    // display: none }`, из-за чего queryByRole не отфильтровывает содержимое
    // закрытого <details>.
    // Проверяем то же самое поведение — «свёрнута по умолчанию» — через
    // атрибут open, который реально меняется при раскрытии.
    setup();
    const summary = screen.getByText('Управление томом');
    expect(summary).toBeInTheDocument();
    expect(summary.closest('details')).not.toHaveAttribute('open');
  });

  it('раскрывается и показывает состояние файла', async () => {
    const { user } = setup();
    await user.click(screen.getByText('Управление томом'));
    expect(screen.getByText('Файл загружен')).toBeInTheDocument();
  });

  it('создаёт страницы по диапазону', async () => {
    const { user, onCreatePages } = setup();
    await user.click(screen.getByText('Управление томом'));
    await user.type(screen.getByLabelText('Диапазон страниц'), '1-5,7');
    await user.click(screen.getByRole('button', { name: 'Создать страницы' }));
    expect(onCreatePages).toHaveBeenCalledWith('1-5,7');
  });

  it('очищает поле диапазона после успешного создания страниц', async () => {
    const { user } = setup();
    await user.click(screen.getByText('Управление томом'));
    const input = screen.getByLabelText('Диапазон страниц');
    await user.type(input, '1-5');
    await user.click(screen.getByRole('button', { name: 'Создать страницы' }));
    expect(await screen.findByLabelText('Диапазон страниц')).toHaveValue('');
  });

  it('не очищает поле диапазона, когда создание не удалось', async () => {
    const { user } = setup({ onCreatePages: vi.fn().mockResolvedValue(false) });
    await user.click(screen.getByText('Управление томом'));
    const input = screen.getByLabelText('Диапазон страниц');
    await user.type(input, '1-5');
    await user.click(screen.getByRole('button', { name: 'Создать страницы' }));
    expect(await screen.findByLabelText('Диапазон страниц')).toHaveValue('1-5');
  });

  it('включает режим перестановки глав', async () => {
    const { user, onReorderingChange } = setup();
    await user.click(screen.getByText('Управление томом'));
    await user.click(screen.getByRole('button', { name: 'Изменить порядок глав' }));
    expect(onReorderingChange).toHaveBeenCalledWith(true);
  });

  it('в режиме перестановки показывает дерево глав', async () => {
    const { user } = setup({ reordering: true });
    await user.click(screen.getByText('Управление томом'));
    expect(screen.getByText('Перетаскивайте главы, чтобы изменить порядок')).toBeInTheDocument();
  });

  it('удаление тома спрашивает подтверждение', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    const { user, onDeleteWork } = setup();
    await user.click(screen.getByText('Управление томом'));
    await user.click(screen.getByRole('button', { name: 'Удалить том' }));
    expect(onDeleteWork).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });
});
