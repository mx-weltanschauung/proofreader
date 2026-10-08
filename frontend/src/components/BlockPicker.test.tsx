import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import { BlockPicker } from './BlockPicker';
import { pagesApi } from '../services/api';

vi.mock('../services/api');

// Сервер уже не отдаёт блоки-сноски вовсе (задача 9, page_blocks.go
// фильтрует их до сериализации — вёрстки для них нет), а тип `PageBlock`
// значения `'footnote'` не знает. Фикстура поэтому состоит только из видов,
// которые реально приходят по проводу; клиентского фильтра нет — фильтровать
// нечего.
const BLOCKS = [
  { start: 0, end: 25, kind: 'paragraph' as const, html: '<p>Первый абзац.</p>' },
  { start: 27, end: 45, kind: 'heading' as const, html: '<h2>Заголовок</h2>' },
  { start: 47, end: 70, kind: 'paragraph' as const, html: '<p>Второй абзац.</p>' },
];

describe('BlockPicker', () => {
  beforeEach(() => {
    vi.mocked(pagesApi.blocks).mockResolvedValue({ data: { blocks: BLOCKS } } as never);
  });

  it('показывает готовый HTML блоков, а не сырой markdown', async () => {
    render(<BlockPicker workId={1} pageId={10} pageNumber={5} onPick={vi.fn()} />);
    expect(await screen.findByText('Первый абзац.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Заголовок' })).toBeInTheDocument();
    expect(screen.queryByText(/^##/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\[\^/)).not.toBeInTheDocument();
  });

  it('пустой список блоков показывает «нечего вклеить», а не пустоту', async () => {
    // Замер на живом корпусе: семь полос из 5106 отдают пустой список блоков
    // при непустом тексте — продолжение аппарата с предыдущей полосы, не
    // поломка. Подборщик обязан сказать это внятно, а не показать пустое
    // место, из которого непонятно, что делать.
    vi.mocked(pagesApi.blocks).mockResolvedValueOnce({ data: { blocks: [] } } as never);
    render(<BlockPicker workId={1} pageId={11} pageNumber={6} onPick={vi.fn()} />);
    expect(await screen.findByText(/нечего вклеить/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /начало вклейки/i })).not.toBeInTheDocument();
  });

  it('отдаёт байтовые границы выбранного блока', async () => {
    const onPick = vi.fn();
    render(<BlockPicker workId={1} pageId={10} pageNumber={5} onPick={onPick} />);
    await screen.findByText('Первый абзац.');
    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[1]);
    expect(onPick).toHaveBeenCalledWith('start', 27, 5);
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[2]);
    expect(onPick).toHaveBeenCalledWith('end', 70, 5);
  });

  it('смена полосы не оставляет блоки прежней на экране', async () => {
    const { rerender } = render(
      <BlockPicker workId={1} pageId={10} pageNumber={5} onPick={vi.fn()} />,
    );
    await screen.findByText('Первый абзац.');

    vi.mocked(pagesApi.blocks).mockResolvedValueOnce({
      data: {
        blocks: [{ start: 0, end: 10, kind: 'paragraph' as const, html: '<p>Соседняя.</p>' }],
      },
    } as never);
    rerender(<BlockPicker workId={1} pageId={12} pageNumber={6} onPick={vi.fn()} />);

    // Прежний текст исчезает немедленно (сброс в теле рендера), новый
    // приезжает следом асинхронно.
    expect(screen.queryByText('Первый абзац.')).not.toBeInTheDocument();
    expect(await screen.findByText('Соседняя.')).toBeInTheDocument();
  });
});
