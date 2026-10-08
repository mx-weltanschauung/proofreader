import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import type { Edition } from '../types';
import { partsForVolume } from '../utils/volumeParts';
import { WorkVolumeFields, type WorkVolumePatch } from './WorkVolumeFields';

const EDITIONS: Edition[] = [
  {
    id: 1,
    title: 'Сочинения, 2-е изд.',
    slug: 'mae-2',
    description: '',
    created_at: '',
    updated_at: '',
  },
];

interface HarnessState {
  editionId: number | null;
  volumeNumber: number | null;
  volumePart: string | null;
  pageOffset: number;
  shelfLabel: string;
}

const DEFAULT_STATE: HarnessState = {
  editionId: null,
  volumeNumber: null,
  volumePart: null,
  pageOffset: 0,
  shelfLabel: '',
};

// WorkVolumeFields — полностью управляемый компонент: без родителя, который
// синхронно прокидывает патч обратно в проп, любой controlled <input> в нём
// откатится к старому значению после правки (так ведёт себя React, а не
// баг компонента). Обёртка ниже воспроизводит именно то, что делает
// настоящий WorkEdit.handleVolumeChange — иначе тест проверяет контракт,
// которого нет ни у одного реального родителя.
function renderFields(overrides: Partial<HarnessState> = {}) {
  const onChange = vi.fn<(patch: WorkVolumePatch) => void>();

  function Harness() {
    const [state, setState] = useState<HarnessState>({ ...DEFAULT_STATE, ...overrides });
    return (
      <WorkVolumeFields
        editions={EDITIONS}
        {...state}
        onChange={(patch) => {
          onChange(patch);
          setState((prev) => ({ ...prev, ...patch }));
        }}
      />
    );
  }

  render(<Harness />);
  return onChange;
}

describe('partsForVolume', () => {
  it('знает, что полутома есть только у 25 и 26', () => {
    // Правило зеркалит multipartVolumes из internal/api/work_volume.go:
    // сервер отвергает полутом у любого другого тома четырёхсотым.
    expect(partsForVolume(25)).toEqual(['I', 'II']);
    expect(partsForVolume(26)).toEqual(['I', 'II', 'III']);
    expect(partsForVolume(4)).toEqual([]);
    expect(partsForVolume(null)).toEqual([]);
  });
});

describe('WorkVolumeFields', () => {
  it('пустой номер тома отдаёт как null, а не как 0', async () => {
    // Ноль — валидное число и невалидный том: сервер отвечает 400 на
    // volume_number < 1. «Поле очищено» обязано доехать как null.
    const onChange = renderFields({ volumeNumber: 4 });

    await userEvent.clear(screen.getByLabelText(/Номер тома/));

    expect(onChange).toHaveBeenCalledWith({ volumeNumber: null });
  });

  it('выключает полутом у тома без полутомов', () => {
    renderFields({ volumeNumber: 4 });

    expect(screen.getByLabelText(/Полутом/)).toBeDisabled();
  });

  it('предлагает I—III только у тома 26', () => {
    renderFields({ volumeNumber: 26 });

    const select = screen.getByLabelText(/Полутом/);
    expect(select).toBeEnabled();
    expect(Array.from(select.querySelectorAll('option')).map((o) => o.textContent)).toEqual([
      '—',
      'I',
      'II',
      'III',
    ]);
  });

  it('сбрасывает полутом, когда номер тома перестаёт его допускать', async () => {
    // Иначе на экране остаётся «т. 4, II» — комбинация, которую сервер
    // отвергнет, а человек увидит только после сохранения.
    renderFields({ volumeNumber: 26, volumePart: 'III' });

    await userEvent.clear(screen.getByLabelText(/Номер тома/));
    await userEvent.type(screen.getByLabelText(/Номер тома/), '4');

    // Патч на шаге «стереть» несёт volumePart: null явно; патч на шаге
    // «напечатать 4» его не повторяет — к этому моменту в состоянии
    // родителя volumePart уже null, повторно сбрасывать нечего. Инвариант
    // проверяем не по форме отдельного патча, а по итоговому виду поля.
    const volumePartSelect = screen.getByLabelText(/Полутом/);
    expect(volumePartSelect).toBeDisabled();
    expect(volumePartSelect).toHaveValue('');
  });

  it('пустое собрание отдаёт как null', async () => {
    const onChange = renderFields({ editionId: 1 });

    await userEvent.selectOptions(screen.getByLabelText(/Собрание/), '');

    expect(onChange).toHaveBeenCalledWith({ editionId: null });
  });
});

describe('WorkVolumeFields — подпись корешка', () => {
  it('показывает сохранённую подпись', () => {
    renderFields({ shelfLabel: '1893—1894' });
    expect(screen.getByLabelText('Подпись корешка')).toHaveValue('1893—1894');
  });

  it('правка подписи уходит патчем', async () => {
    const onChange = renderFields();
    await userEvent.type(screen.getByLabelText('Подпись корешка'), 'Что делать?');
    expect(onChange).toHaveBeenCalled();
    expect(screen.getByLabelText('Подпись корешка')).toHaveValue('Что делать?');
  });

  // Предел тот же, что у сервера: форма не должна давать набрать то, что
  // всё равно вернётся четырёхсотым.
  it('не даёт набрать больше 120 знаков', () => {
    renderFields();
    expect(screen.getByLabelText('Подпись корешка')).toHaveAttribute('maxLength', '120');
  });
});
