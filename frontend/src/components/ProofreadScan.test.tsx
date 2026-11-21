import { render, screen, fireEvent } from '@testing-library/react';
import { it, expect, vi } from 'vitest';
import { ProofreadScan } from './ProofreadScan';
it('places the selected finding at normalized scan coordinates and zooms the full scan', () => {
  render(<ProofreadScan url="/scan" number={126} box={[0.2, 0.3, 0.1, 0.05]} refresh={vi.fn()} />);
  expect(screen.getByRole('img', { name: 'Скан полосы 126' })).toBeVisible();
  expect(screen.getByLabelText('Место находки на скане')).toHaveStyle({ left: '20%', top: '30%' });
  fireEvent.click(screen.getByRole('button', { name: 'Увеличить' }));
  expect(screen.getByText('150%')).toBeVisible();
});
