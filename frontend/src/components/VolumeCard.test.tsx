import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { VolumeCard } from './VolumeCard';
import type { VolumeSummary } from '../types';

const ANCHOR = { left: 100, top: 100, right: 164, bottom: 312 } as DOMRect;

function volume(over: Partial<VolumeSummary> = {}): VolumeSummary {
  return {
    id: 1,
    title: 'Том 1',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '',
    volume_number: 1,
    pages_total: 600,
    pages_by_status: { вычитана: 300 },
    chapters_total: 12,
    top_chapters: [
      { title: 'ЧТО ТАКОЕ «ДРУЗЬЯ НАРОДА»', pages: 222, share: 0.42 },
      { title: 'ЭКОНОМИЧЕСКОЕ СОДЕРЖАНИЕ НАРОДНИЧЕСТВА', pages: 188, share: 0.35 },
    ],
    ...over,
  } as VolumeSummary;
}

function card(over: Partial<VolumeSummary> = {}) {
  return render(<VolumeCard volume={volume(over)} anchor={ANCHOR} id="volume-card" />);
}

describe('VolumeCard', () => {
  it('перечисляет работы тома с объёмом', () => {
    const { container } = card();
    const works = [...container.querySelectorAll('.volume-card-work')];
    expect(works.map((w) => w.textContent)).toEqual([
      'ЧТО ТАКОЕ «ДРУЗЬЯ НАРОДА»222 с.',
      'ЭКОНОМИЧЕСКОЕ СОДЕРЖАНИЕ НАРОДНИЧЕСТВА188 с.',
    ]);
  });

  // Выведенная подпись — это заглавия тех же работ, что перечислены строкой
  // ниже. Повторённая, она съедала карточку целиком: у ленинского тома 1 в неё
  // не помещались ни список работ, ни сводка.
  it('не повторяет подпись, выведенную из работ', () => {
    const { container } = card();
    expect(container.querySelector('.volume-card-label')).toBeNull();
    expect(container.textContent!.match(/ЧТО ТАКОЕ «ДРУЗЬЯ НАРОДА»/g)).toHaveLength(1);
  });

  // Ручная подпись из списка работ не выводится, и повторить её нечем — она
  // единственное место, где сказано, чем том является.
  it('показывает подпись, заданную руками', () => {
    const { container } = card({ shelf_label: 'Первые работы' });
    expect(container.querySelector('.volume-card-label')!.textContent).toBe('Первые работы');
  });

  // Сводка стоит последней и именно поэтому первой пропадала под обрезкой.
  it('показывает сводку по тому, без доли вычитки', () => {
    const { container } = card();
    expect(container.querySelector('.volume-card-stats')!.textContent).toBe(
      '600 страниц · 12 работ',
    );
  });
});
