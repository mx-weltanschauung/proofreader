import { describe, it, expect } from 'vitest';
import type { SearchVolume } from '../types';
import { orderVolumes } from './searchVolumes';

function vol(work_id: number, parent_work_id: number | null = null): SearchVolume {
  return {
    work_id,
    title: `w${work_id}`,
    author: '',
    volume_label: '',
    edition_id: 1,
    edition_title: '',
    role: parent_work_id ? 'front_matter' : 'volume',
    parent_work_id,
    text_hits: 1,
    apparatus_hits: 0,
    numbering_style: 'arabic',
    pages: [],
  };
}

describe('orderVolumes', () => {
  it('ставит служебную работу сразу под родителем, порядок остальных не трогает', () => {
    const got = orderVolumes([vol(20), vol(11, 10), vol(10), vol(30)]).map((v) => v.work_id);
    expect(got).toEqual([20, 10, 11, 30]);
  });

  it('сирота без родителя в списке остаётся на своём месте', () => {
    const got = orderVolumes([vol(11, 10), vol(30)]).map((v) => v.work_id);
    expect(got).toEqual([11, 30]);
  });
});
