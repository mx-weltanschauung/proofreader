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
  const xAxis = within(chart).getByRole('group', { name: 'Даты по оси X' });
  expect(within(xAxis).getByText('23.09')).toHaveAttribute('datetime', '2026-09-23');
  expect(within(xAxis).getByText('29.09')).toHaveAttribute('datetime', '2026-09-29');
  const yAxis = within(chart).getByRole('group', { name: 'Значения по оси Y' });
  expect(
    within(yAxis)
      .getAllByText(/^(50|40|30|20|10|0)$/)
      .map((tick) => tick.textContent),
  ).toEqual(['50', '40', '30', '20', '10', '0']);
  const seo = within(chart).getByRole('button', { name: 'Краулеры' });
  expect(seo).toHaveAttribute('aria-pressed', 'true');
  const before = Number(chart.querySelector('rect.stats-bar-spa')?.getAttribute('height'));
  fireEvent.click(seo);
  expect(seo).toHaveAttribute('aria-pressed', 'false');
  expect(chart.querySelectorAll('rect.stats-bar-seo')).toHaveLength(0);
  const after = Number(chart.querySelector('rect.stats-bar-spa')?.getAttribute('height'));
  expect(after).toBeGreaterThan(before);
  expect(within(yAxis).getByText('4')).toBeInTheDocument();
});

it('одной кнопкой убирает все каналы, затем показывает выбранный канал', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-29T12:00:00Z'));
  render(
    <TrafficChart
      rows={[
        { day: '2026-09-29T00:00:00Z', channel: 'spa', views: 4, visitors: 2 },
        { day: '2026-09-29T00:00:00Z', channel: 'seo', views: 40, visitors: 1 },
      ]}
      days={7}
      onDaysChange={vi.fn()}
    />,
  );
  const chart = screen.getByRole('region', { name: 'График посещаемости' });
  fireEvent.click(within(chart).getByRole('button', { name: 'Убрать все каналы' }));
  expect(within(chart).getByText('Выберите канал')).toBeInTheDocument();
  const spa = within(chart).getByRole('button', { name: 'Читатели' });
  const seo = within(chart).getByRole('button', { name: 'Краулеры' });
  expect(spa).toHaveAttribute('aria-pressed', 'false');
  expect(seo).toHaveAttribute('aria-pressed', 'false');
  fireEvent.click(spa);
  expect(spa).toHaveAttribute('aria-pressed', 'true');
  expect(seo).toHaveAttribute('aria-pressed', 'false');
  expect(chart.querySelectorAll('rect.stats-bar-spa')).toHaveLength(1);
  expect(chart.querySelectorAll('rect.stats-bar-seo')).toHaveLength(0);
  expect(
    within(within(chart).getByRole('group', { name: 'Значения по оси Y' })).getByText('4'),
  ).toBeInTheDocument();
});

it('подписывает ось Y целым числом просмотров', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-29T12:00:00Z'));
  render(
    <TrafficChart
      rows={[{ day: '2026-09-29T00:00:00Z', channel: 'spa', views: 11, visitors: 2 }]}
      days={30}
      onDaysChange={vi.fn()}
    />,
  );
  const yAxis = screen.getByRole('group', { name: 'Значения по оси Y' });
  expect(
    within(yAxis)
      .getAllByText(/./)
      .every((tick) => /^\d+$/.test(tick.textContent ?? '')),
  ).toBe(true);
});
