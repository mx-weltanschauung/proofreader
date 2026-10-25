import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ClosedBadge } from './ClosedBadge';

describe('ClosedBadge', () => {
  it('без сведений о закрытии — просто «закрыт»', () => {
    render(<ClosedBadge />);
    expect(screen.getByText('закрыт')).toBeInTheDocument();
  });

  it('с закрытием печатает дату по Москве и причину', () => {
    render(<ClosedBadge closure={{ at: '2026-10-23T22:30:00Z', reason: 'письмо издателя' }} />);
    // 22:30 UTC — уже 24-е по Москве.
    expect(screen.getByText('закрыто 24.10.2026: письмо издателя')).toBeInTheDocument();
  });

  it('редактору причину не печатает', () => {
    render(<ClosedBadge closure={{ at: '2026-10-23T10:00:00Z', reason: '' }} />);
    expect(screen.getByText('закрыто 23.10.2026')).toBeInTheDocument();
  });
});
