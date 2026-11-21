import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { TrafficChart } from './TrafficChart';

afterEach(() => vi.useRealTimers());

it('переключает каналы и сохраняет нулевые календарные дни', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-29T12:00:00Z'));
  const rows = [
    { day: '2026-09-29T00:00:00Z', channel: 'spa', views: 4, visitors: 2 },
    { day: '2026-09-29T00:00:00Z', channel: 'seo', views: 40, visitors: 1 },
  ];
  render(<TrafficChart rows={rows} days={7} onDaysChange={vi.fn()} />);
  const chart = screen.getByRole('region', { name: 'График посещаемости' });
  expect(chart.querySelectorAll('[data-day]')).toHaveLength(7);
  expect(chart.querySelector('[data-day="2026-09-28"]')).toHaveAttribute('data-total', '0');
  const seo = within(chart).getByRole('button', { name: 'Краулеры' });
  expect(seo).toHaveAttribute('aria-pressed', 'true');
  const before = Number(chart.querySelector('rect.stats-bar-spa')?.getAttribute('height'));
  fireEvent.click(seo);
  expect(seo).toHaveAttribute('aria-pressed', 'false');
  expect(chart.querySelectorAll('rect.stats-bar-seo')).toHaveLength(0);
  const after = Number(chart.querySelector('rect.stats-bar-spa')?.getAttribute('height'));
  expect(after).toBeGreaterThan(before);
});

it('все каналы можно выключить', () => {
  render(<TrafficChart rows={[]} days={7} onDaysChange={vi.fn()} />);
  const chart = screen.getByRole('region', { name: 'График посещаемости' });
  within(chart)
    .getAllByRole('button', { pressed: true })
    .filter((b) => b.textContent !== '7 дней')
    .forEach((b) => fireEvent.click(b));
  expect(within(chart).getByText('Выберите канал')).toBeInTheDocument();
});
