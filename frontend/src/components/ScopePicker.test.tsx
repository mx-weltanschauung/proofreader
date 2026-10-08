import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ScopePicker } from './ScopePicker';
import { EMPTY_SCOPE } from '../utils/searchScope';

const shelf = {
  editions: [
    {
      edition: { id: 2, title: 'К. Маркс и Ф. Энгельс. Сочинения' },
      volumes: [
        { id: 13, title: 'Том 13', volume_number: 13 },
        { id: 14, title: 'Том 14', volume_number: 14 },
        // Служебные работы — оба вида признака сразу: `isServiceWork`
        // дизъюнктивен, и без обеих строк одна из его половин осталась бы
        // без теста. `/api/shelf` их сегодня и не отдаёт (у служебной работы
        // нет своего edition_id — фильтр SQL режет по нему), но фильтр здесь
        // второй слой обороны, а не единственный, и должен пережить смену
        // устройства сервера.
        { id: 15, title: 'Служебный (дитя тома)', volume_number: 15, parent_work_id: 13 },
        { id: 16, title: 'Служебный (передние листы)', volume_number: 16, role: 'front_matter' },
      ],
    },
  ],
  loose_works: [],
};

vi.mock('../services/api', () => ({
  shelfApi: { get: vi.fn(() => Promise.resolve({ data: shelf })) },
}));

describe('ScopePicker', () => {
  it('галочка собрания кладёт его в область', async () => {
    const onChange = vi.fn();
    render(<ScopePicker value={EMPTY_SCOPE} onChange={onChange} />);
    await userEvent.click(await screen.findByRole('checkbox', { name: /Маркс/ }));
    expect(onChange).toHaveBeenCalledWith({ editions: [2], works: [], chapters: [] });
  });

  it('галочка тома кладёт том, не собрание', async () => {
    const onChange = vi.fn();
    render(<ScopePicker value={EMPTY_SCOPE} onChange={onChange} />);
    await userEvent.click(await screen.findByRole('button', { name: /показать тома/i }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Том 13' }));
    expect(onChange).toHaveBeenCalledWith({ editions: [], works: [13], chapters: [] });
  });

  it('снятие последней галочки возвращает всю читальню', async () => {
    const onChange = vi.fn();
    render(<ScopePicker value={{ editions: [2], works: [], chapters: [] }} onChange={onChange} />);
    await userEvent.click(await screen.findByRole('checkbox', { name: /Маркс/ }));
    expect(onChange).toHaveBeenCalledWith(EMPTY_SCOPE);
  });

  it('смена тома сбрасывает выбранные главы', async () => {
    const onChange = vi.fn();
    render(
      <ScopePicker value={{ editions: [], works: [13], chapters: [101] }} onChange={onChange} />,
    );
    await userEvent.click(await screen.findByRole('button', { name: /показать тома/i }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Том 14' }));
    expect(onChange).toHaveBeenCalledWith({ editions: [], works: [13, 14], chapters: [] });
  });

  it('служебные работы не появляются среди томов, обычные — появляются', async () => {
    render(<ScopePicker value={EMPTY_SCOPE} onChange={vi.fn()} />);
    await userEvent.click(await screen.findByRole('button', { name: /показать тома/i }));
    // Утверждаем оба факта разом: одно «ничего служебного не показалось» само
    // по себе прошло бы и при сломанном рендере списка томов целиком.
    expect(screen.getByRole('checkbox', { name: 'Том 13' })).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Том 14' })).toBeInTheDocument();
    expect(
      screen.queryByRole('checkbox', { name: /Служебный \(дитя тома\)/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('checkbox', { name: /Служебный \(передние листы\)/ }),
    ).not.toBeInTheDocument();
  });

  // Брифинг проверяет "вызван один раз" простым expect — но к этому месту
  // счётчик общего мока модуля уже единица от предыдущих тестов файла, и
  // проверка прошла бы даже без кэша вовсе. Настоящая проверка требует
  // свежего модуля: vi.resetModules() + динамический импорт заново на
  // каждый тест, иначе кэш `inFlight` пережил бы и сброс моков.
  describe('кэш полки', () => {
    beforeEach(() => {
      vi.resetModules();
      vi.clearAllMocks();
    });

    it('полка запрашивается один раз на несколько отрисовок', async () => {
      const { ScopePicker: FreshScopePicker } = await import('./ScopePicker');
      const { shelfApi: freshShelfApi } = await import('../services/api');

      const { unmount } = render(<FreshScopePicker value={EMPTY_SCOPE} onChange={vi.fn()} />);
      await screen.findByRole('checkbox', { name: /Маркс/ });
      unmount();
      render(<FreshScopePicker value={EMPTY_SCOPE} onChange={vi.fn()} />);
      await screen.findByRole('checkbox', { name: /Маркс/ });
      expect(freshShelfApi.get).toHaveBeenCalledTimes(1);
    });
  });
});
