import { describe, it, expect, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PageJump } from './PageJump';

function setup(
  onJump: (input: string) => Promise<string | null> = vi.fn(async () => null),
  props: Partial<React.ComponentProps<typeof PageJump>> = {},
) {
  const user = userEvent.setup();
  render(
    <>
      <input aria-label="Чужое поле" />
      <PageJump currentLabel="123" onJump={onJump} {...props} />
    </>,
  );
  return { user, onJump };
}

const trigger = () => screen.getByRole('button', { name: /Перейти к странице/ });

describe('PageJump', () => {
  it('показывает видимую страницу', () => {
    setup();
    expect(trigger()).toHaveTextContent('с. 123');
  });

  it('по нажатию становится полем ввода с фокусом', async () => {
    const { user } = setup();
    await user.click(trigger());
    expect(screen.getByRole('textbox', { name: 'Номер страницы' })).toHaveFocus();
  });

  it('Enter отдаёт набранное и закрывает поле при успехе', async () => {
    const { user, onJump } = setup();
    await user.click(trigger());
    await user.keyboard('45{Enter}');
    expect(onJump).toHaveBeenCalledWith('45');
    expect(screen.queryByRole('textbox', { name: 'Номер страницы' })).toBeNull();
    expect(trigger()).toBeInTheDocument();
  });

  it('при отказе оставляет поле и говорит почему', async () => {
    const { user } = setup(async () => 'В томе нет страницы 900');
    await user.click(trigger());
    await user.keyboard('900{Enter}');
    expect(screen.getByRole('alert')).toHaveTextContent('В томе нет страницы 900');
    expect(screen.getByRole('textbox', { name: 'Номер страницы' })).toBeInTheDocument();
  });

  it('пока переход думает, поле остаётся в фокусе и не отключается', async () => {
    let finish!: (value: string | null) => void;
    const { user } = setup(() => new Promise((resolve) => (finish = resolve)));
    await user.click(trigger());
    await user.keyboard('900{Enter}');
    const field = screen.getByRole('textbox', { name: 'Номер страницы' });
    expect(field).not.toBeDisabled();
    expect(field).toHaveFocus();
    await act(async () => finish('В томе нет страницы 900'));
    expect(field).toHaveFocus();
  });

  it('после отказа Escape закрывает поле', async () => {
    const { user } = setup(async () => 'В томе нет страницы 900');
    await user.click(trigger());
    await user.keyboard('900{Enter}');
    await screen.findByRole('alert');
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('textbox', { name: 'Номер страницы' })).toBeNull();
  });

  it('пустой Enter никуда не ходит', async () => {
    const { user, onJump } = setup();
    await user.click(trigger());
    await user.keyboard('{Enter}');
    expect(onJump).not.toHaveBeenCalled();
  });

  it('Escape закрывает поле и возвращает фокус на кнопку', async () => {
    const { user, onJump } = setup();
    await user.click(trigger());
    await user.keyboard('45{Escape}');
    expect(onJump).not.toHaveBeenCalled();
    expect(screen.queryByRole('textbox', { name: 'Номер страницы' })).toBeNull();
    expect(trigger()).toHaveFocus();
  });

  it('уход фокуса закрывает поле', async () => {
    const { user } = setup();
    await user.click(trigger());
    await user.click(screen.getByRole('textbox', { name: 'Чужое поле' }));
    expect(screen.queryByRole('textbox', { name: 'Номер страницы' })).toBeNull();
  });

  it('клавиша g открывает поле, не печатая себя в него', async () => {
    const { user } = setup();
    await user.keyboard('g');
    const field = screen.getByRole('textbox', { name: 'Номер страницы' });
    expect(field).toHaveFocus();
    expect(field).toHaveValue('');
  });

  it('в русской раскладке та же клавиша — «п» — тоже открывает поле', () => {
    setup();
    fireEvent.keyDown(document.body, { key: 'п', code: 'KeyG' });
    expect(screen.getByRole('textbox', { name: 'Номер страницы' })).toBeInTheDocument();
  });

  it('Escape в поле не уходит дальше — выход из чтения его не получает', async () => {
    const onWindowKey = vi.fn();
    window.addEventListener('keydown', onWindowKey);
    const { user } = setup();
    await user.click(trigger());
    onWindowKey.mockClear();
    await user.keyboard('{Escape}');
    window.removeEventListener('keydown', onWindowKey);
    expect(onWindowKey).not.toHaveBeenCalled();
  });

  it('клавиша g в чужом поле ввода — просто буква', async () => {
    const { user } = setup();
    await user.click(screen.getByRole('textbox', { name: 'Чужое поле' }));
    await user.keyboard('g');
    expect(screen.queryByRole('textbox', { name: 'Номер страницы' })).toBeNull();
  });

  it('клавиша g с модификатором не перехватывается', async () => {
    const { user } = setup();
    await user.keyboard('{Control>}g{/Control}');
    expect(screen.queryByRole('textbox', { name: 'Номер страницы' })).toBeNull();
  });
});
