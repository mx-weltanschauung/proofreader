import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ChapterFacet } from './ChapterFacet';

const chapters = [
  { id: 101, title: 'Первая', hits: 12 },
  { id: 102, title: 'Вторая', hits: 3 },
];

describe('ChapterFacet', () => {
  it('печатает главы с числом попаданий', () => {
    render(<ChapterFacet chapters={chapters} total={2} selected={[]} onToggle={vi.fn()} />);
    expect(screen.getByRole('button', { name: /Первая/ })).toHaveTextContent('12');
  });

  it('выбранная глава помечена и снимается повторным нажатием', async () => {
    const onToggle = vi.fn();
    render(<ChapterFacet chapters={chapters} total={2} selected={[101]} onToggle={onToggle} />);
    const first = screen.getByRole('button', { name: /Первая/ });
    expect(first).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(first);
    expect(onToggle).toHaveBeenCalledWith(101);
  });

  it('остаток называется числом, а не прячется', () => {
    render(<ChapterFacet chapters={chapters} total={47} selected={[]} onToggle={vi.fn()} />);
    expect(screen.getByText(/ещё 45/)).toBeInTheDocument();
  });

  it('одна-единственная глава фасета не рисует: сужать нечего', () => {
    const { container } = render(
      <ChapterFacet chapters={[chapters[0]]} total={1} selected={[]} onToggle={vi.fn()} />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
