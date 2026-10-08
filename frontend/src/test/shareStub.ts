import { expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

/**
 * Меню системы для проверки проводки «Поделиться» на экранах. jsdom не знает
 * `navigator.share`; подделка его ставит и отдаёт шпиона, чтобы тест экрана
 * сверил, ЧТО экран передал кнопке (подпись и канонический адрес), а не
 * поведение самой кнопки — оно проверено в ShareButton.test.tsx.
 */
export function stubShare() {
  const share = vi.fn(async (_data: ShareData) => {});
  Object.defineProperty(navigator, 'share', { configurable: true, value: share });
  Object.defineProperty(navigator, 'canShare', { configurable: true, value: () => true });
  return share;
}

export function unstubShare() {
  Object.defineProperty(navigator, 'share', { configurable: true, value: undefined });
  Object.defineProperty(navigator, 'canShare', { configurable: true, value: undefined });
}

/** Нажимает «Поделиться» и возвращает то, что ушло в меню системы. */
export async function shareFrom(share: ReturnType<typeof stubShare>): Promise<ShareData> {
  await userEvent.click(screen.getByRole('button', { name: 'Поделиться' }));
  expect(share).toHaveBeenCalledTimes(1);
  return share.mock.calls[0][0];
}
