import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FilterCombobox, type ComboOption } from './FilterCombobox';

const VOLUMES: ComboOption[] = [
  { id: 1, label: 'Том 1', group: 'Плеханов', search: 'Плеханов Том 1' },
  { id: 14, label: 'Том 14', group: 'Плеханов', search: 'Плеханов Том 14' },
  { id: 10, label: 'Том 10', group: 'Плеханов', search: 'Плеханов Том 10' },
  { id: 101, label: 'Том 1', group: 'Ленин', search: 'Ленин Том 1' },
  { id: 500, label: 'Отдельная работа', group: 'Отдельные работы' },
];

// Порядок чтения, дерево через глубину: у «Письма первого» и «Письма
// второго» один родитель, «Искусство и общественная жизнь» — отдельный корень.
const CHAPTERS: ComboOption[] = [
  { id: 1, label: 'Письма без адреса', depth: 0 },
  { id: 2, label: 'Письмо первое', depth: 1 },
  { id: 3, label: 'Письмо второе', depth: 1 },
  { id: 4, label: 'Искусство и общественная жизнь', depth: 0 },
  { id: 5, label: 'Ещё о ёмкости', depth: 0 },
];

function Harness({
  options,
  onPick,
}: {
  options: ComboOption[];
  onPick?: (id: number | '') => void;
}) {
  const [value, setValue] = useState<number | ''>('');
  return (
    <>
      <label htmlFor="combo">Том</label>
      <FilterCombobox
        id="combo"
        options={options}
        value={value}
        onChange={(id) => {
          setValue(id);
          onPick?.(id);
        }}
      />
    </>
  );
}

describe('FilterCombobox', () => {
  it('раскрывает список по фокусу и группирует по собраниям', async () => {
    render(<Harness options={VOLUMES} />);
    await userEvent.click(screen.getByRole('combobox', { name: 'Том' }));

    expect(screen.getAllByRole('option')).toHaveLength(5);
    expect(screen.getByText('Плеханов')).toBeTruthy();
    expect(screen.getByText('Отдельные работы')).toBeTruthy();
  });

  it('выбирает найденное кликом и показывает выбранное в поле', async () => {
    const onPick = vi.fn();
    render(<Harness options={VOLUMES} onPick={onPick} />);
    const input = screen.getByRole('combobox', { name: 'Том' });

    await userEvent.type(input, 'плех 14');
    expect(screen.getAllByRole('option')).toHaveLength(1);
    await userEvent.click(screen.getByRole('option', { name: 'Том 14' }));

    expect(onPick).toHaveBeenCalledWith(14);
    expect(screen.queryByRole('listbox')).toBeNull();
    expect((input as HTMLInputElement).value).toBe('Том 14');
  });

  it('выбирает с клавиатуры: стрелка вниз и Enter', async () => {
    const onPick = vi.fn();
    render(<Harness options={VOLUMES} onPick={onPick} />);
    const input = screen.getByRole('combobox', { name: 'Том' });

    await userEvent.type(input, 'плех');
    await userEvent.keyboard('{ArrowDown}{Enter}');

    // Первая стрелка уводит с первой строки на вторую.
    expect(onPick).toHaveBeenCalledWith(14);
  });

  it('Esc закрывает список и возвращает в поле выбранное', async () => {
    render(<Harness options={VOLUMES} />);
    const input = screen.getByRole('combobox', { name: 'Том' });

    await userEvent.type(input, 'ленин');
    await userEvent.keyboard('{Escape}');

    expect(screen.queryByRole('listbox')).toBeNull();
    expect((input as HTMLInputElement).value).toBe('');
  });

  it('честно говорит, что ничего не нашлось', async () => {
    render(<Harness options={VOLUMES} />);
    await userEvent.type(screen.getByRole('combobox', { name: 'Том' }), 'гегель');

    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.getByText('Ничего не найдено')).toBeTruthy();
  });

  it('рисует вложенность глав отступом', async () => {
    render(<Harness options={CHAPTERS} />);
    await userEvent.type(screen.getByRole('combobox', { name: 'Том' }), 'второе');

    const [parent, child] = screen.getAllByRole('option');
    expect(parent.textContent).toBe('Письма без адреса');
    expect(child.style.paddingLeft).not.toBe(parent.style.paddingLeft);
  });
});
