import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { AxiosResponse } from 'axios';
import { settingsApi } from '../../services/api';
import type { SiteSettings } from '../../types';
import { SettingsAdmin } from './SettingsAdmin';

vi.mock('../../services/api', () => ({
  settingsApi: { get: vi.fn(), update: vi.fn() },
}));

function ok(data: SiteSettings): Promise<AxiosResponse<SiteSettings>> {
  return Promise.resolve({ data } as AxiosResponse<SiteSettings>);
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(settingsApi.get).mockReturnValue(ok({ suggestions_per_hour: 5 }));
});

describe('SettingsAdmin', () => {
  it('показывает сохранённый предел и объясняет скользящий час', async () => {
    render(<SettingsAdmin />);
    expect(
      await screen.findByRole('spinbutton', { name: 'Предложений правок за час' }),
    ).toHaveValue(5);
    expect(screen.getByText(/скользящий час/i)).toBeInTheDocument();
  });

  it('сохраняет новое число и показывает ответ сервера', async () => {
    vi.mocked(settingsApi.update).mockReturnValue(ok({ suggestions_per_hour: 7 }));
    render(<SettingsAdmin />);
    const input = await screen.findByRole('spinbutton', { name: 'Предложений правок за час' });
    fireEvent.change(input, { target: { value: '7' } });
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(settingsApi.update).toHaveBeenCalledWith(7));
    expect(await screen.findByText('Сохранено')).toBeInTheDocument();
    expect(input).toHaveValue(7);
  });

  it.each(['', '0', '-1', '1.5'])('не отправляет недопустимое значение %s', async (value) => {
    render(<SettingsAdmin />);
    const input = await screen.findByRole('spinbutton', { name: 'Предложений правок за час' });
    fireEvent.change(input, { target: { value } });
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled();
    expect(settingsApi.update).not.toHaveBeenCalled();
  });

  it('показывает ошибку загрузки', async () => {
    vi.mocked(settingsApi.get).mockRejectedValue(new Error('db down'));
    render(<SettingsAdmin />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить настройки');
    expect(screen.queryByRole('button', { name: 'Сохранить' })).toBeNull();
  });

  it('показывает ошибку сохранения и сохраняет ввод', async () => {
    vi.mocked(settingsApi.update).mockRejectedValue(new Error('db down'));
    render(<SettingsAdmin />);
    const input = await screen.findByRole('spinbutton', { name: 'Предложений правок за час' });
    fireEvent.change(input, { target: { value: '7' } });
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось сохранить настройки');
    expect(input).toHaveValue(7);
    expect(screen.queryByText('Сохранено')).toBeNull();
  });
});
