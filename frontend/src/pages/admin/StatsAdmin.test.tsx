import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi } from 'vitest';
import { StatsAdmin } from './StatsAdmin';
import { calendarTotals } from '../../utils/statsTotals';
import { statsApi } from '../../services/api';

vi.mock('../../services/api', () => ({
  statsApi: {
    traffic: vi.fn().mockResolvedValue({
      data: [
        { day: '2026-09-28T00:00:00Z', channel: 'spa', views: 10, visitors: 4 },
        { day: '2026-09-29T00:00:00Z', channel: 'spa', views: 6, visitors: 3 },
        { day: '2026-09-29T00:00:00Z', channel: 'seo', views: 40, visitors: 2 },
      ],
    }),
    top: vi.fn().mockResolvedValue({
      data: [
        {
          work_id: 49,
          entity_id: 0,
          slug_key: '',
          title: 'Что делать?',
          work_title: 'Что делать?',
          missing: false,
          views: 7,
          visitors: 5,
        },
        {
          work_id: 999,
          entity_id: 0,
          slug_key: '',
          title: '',
          work_title: '',
          missing: true,
          views: 2,
          visitors: 1,
        },
      ],
    }),
    referrers: vi.fn().mockResolvedValue({ data: [{ key: 'yandex.ru', views: 3, visitors: 3 }] }),
    searches: vi.fn().mockResolvedValue({
      data: {
        frequent: [{ query: 'ленин', count: 5, zero_hits: 0 }],
        zero: [{ query: 'хрестоматия', count: 2, zero_hits: 2 }],
      },
    }),
    crawlers: vi.fn().mockResolvedValue({
      data: [{ day: '2026-09-29T00:00:00Z', agent: 'YandexBot', views: 40 }],
    }),
    devices: vi.fn().mockResolvedValue({
      data: [
        { day: '2026-09-28T00:00:00Z', device: 'narrow', views: 9, visitors: 3 },
        { day: '2026-09-29T00:00:00Z', device: 'narrow', views: 4, visitors: 3 },
        { day: '2026-09-29T00:00:00Z', device: 'wide', views: 5, visitors: 2 },
        { day: '2026-09-29T00:00:00Z', device: '', views: 20, visitors: 8 },
      ],
    }),
    health: vi.fn().mockResolvedValue({
      data: {
        started_at: '2026-09-29T08:00:00Z',
        routes: [
          {
            route: '/api/search',
            requests: 12,
            status_5xx: 1,
            status_503: 1,
            p50_ms: 40,
            p95_ms: 900,
          },
        ],
        values: { 'limiter_in_use{pool="search"}': 1 },
      },
    }),
  },
}));

describe('StatsAdmin', () => {
  it('показывает итоги, топ со снятым, источники, нулевые запросы и здоровье', async () => {
    render(
      <MemoryRouter>
        <StatsAdmin />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('link', { name: 'Что делать?' })).toHaveAttribute(
      'href',
      '/works/49',
    );
    expect(screen.getByText('снято (#999)')).toBeInTheDocument();
    expect(screen.getByText('yandex.ru')).toBeInTheDocument();
    const zero = screen.getByRole('region', { name: 'Ищут и не находят' });
    expect(within(zero).getByText('хрестоматия')).toBeInTheDocument();
    expect(screen.getByText('/api/search')).toBeInTheDocument();
    // Подпись честная: за период больше суток — посетителе-дни
    expect(screen.getAllByText(/посетителе-дни/).length).toBeGreaterThan(0);
    // Поиск сайта уже считается заходами на /search, вызовы MCP — своим каналом:
    // отдельной серии «Поиск» в легенде нет, иначе один запрос считался бы дважды.
    expect(screen.queryByText('Поиск')).not.toBeInTheDocument();
    expect(screen.getByText('MCP')).toBeInTheDocument();
    expect(screen.getByText('Каталог OPDS')).toBeInTheDocument();
  });

  it('показывает долю экранов по посетителе-дням', async () => {
    render(
      <MemoryRouter>
        <StatsAdmin />
      </MemoryRouter>,
    );
    const section = await screen.findByRole('region', { name: 'Устройства' });
    const phone = await within(section).findByText('Телефон');
    const row = phone.closest('tr')!;
    // 3 + 3 = 6 посетителе-дней из 8 известных → 75 %; по просмотрам вышло бы
    // 72 %, а со «Не указано» в знаменателе — 38 %.
    expect(within(row).getByText('6')).toBeInTheDocument();
    expect(within(row).getByText('75%')).toBeInTheDocument();
    expect(within(section).getByText('Компьютер')).toBeInTheDocument();
  });

  it('«Не указано» показывает число, но в долю не входит', async () => {
    render(
      <MemoryRouter>
        <StatsAdmin />
      </MemoryRouter>,
    );
    const section = await screen.findByRole('region', { name: 'Устройства' });
    const unknown = (await within(section).findByText('Не указано')).closest('tr')!;
    expect(within(unknown).getByText('8')).toBeInTheDocument();
    expect(within(unknown).getByText('—')).toBeInTheDocument();
    expect(within(unknown).queryByText(/%/)).not.toBeInTheDocument();
  });
});

describe('StatsAdmin: сбой одного раздела', () => {
  it('ошибка referrers не гасит топ и здоровье', async () => {
    vi.mocked(statsApi.referrers).mockRejectedValueOnce(new Error('boom'));
    render(
      <MemoryRouter>
        <StatsAdmin />
      </MemoryRouter>,
    );
    const from = await screen.findByRole('region', { name: 'Откуда' });
    expect(await within(from).findByText('Не удалось загрузить')).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Что делать?' })).toBeInTheDocument();
    expect(screen.getByText('/api/search')).toBeInTheDocument();
  });
});

describe('calendarTotals', () => {
  const rows = [
    { day: '2026-09-29T00:00:00Z', channel: 'spa', views: 6, visitors: 3 },
    { day: '2026-09-29T00:00:00Z', channel: 'seo', views: 40, visitors: 2 },
    { day: '2026-09-20T00:00:00Z', channel: 'spa', views: 5, visitors: 1 },
    { day: '2026-08-01T00:00:00Z', channel: 'spa', views: 9, visitors: 9 },
  ];

  it('без визитов сегодня «сегодня» пусто, вчера входит в неделю', () => {
    const t = calendarTotals(rows, '2026-09-30');
    expect(t.day).toEqual({ views: 0, visitors: 0 });
    expect(t.week).toEqual({ views: 6, visitors: 3 });
  });

  it('окно недели — семь календарных дней, дыры его не растягивают', () => {
    // 20.09 вне окна 24—30.09, но внутри окна 30 дней (1—30.09)
    const t = calendarTotals(rows, '2026-09-30');
    expect(t.week.views).toBe(6);
    expect(t.month).toEqual({ views: 11, visitors: 4 });
  });
});
