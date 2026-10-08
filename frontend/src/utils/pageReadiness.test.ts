import { describe, it, expect } from 'vitest';
import type { PageMapEntry, PageStatus } from '../types';
import { readinessLabel, readinessSegments } from './pageReadiness';

function pages(...statuses: PageStatus[]): PageMapEntry[] {
  return statuses.map((status, i) => ({ page_number: i + 1, status }));
}

describe('readinessSegments', () => {
  it('нетронутый диапазон не даёт ни одного сегмента', () => {
    expect(readinessSegments(pages('не_вычитана', 'не_вычитана'))).toEqual([]);
  });

  it('пустой диапазон не даёт сегментов', () => {
    expect(readinessSegments([])).toEqual([]);
  });

  it('пустые страницы не считаются работой: вычитывать в них нечего', () => {
    expect(readinessSegments(pages('пустая_страница', 'не_вычитана'))).toEqual([]);
  });

  it('доля считается от всего диапазона, включая невычитанные', () => {
    const [segment] = readinessSegments(
      pages('вычитана', 'не_вычитана', 'не_вычитана', 'не_вычитана'),
    );
    expect(segment.status).toBe('вычитана');
    expect(segment.count).toBe(1);
    expect(segment.share).toBeCloseTo(0.25);
  });

  it('порядок сегментов — от готового к тревожному, как в легенде тома', () => {
    const segments = readinessSegments(
      pages('требует_внимания', 'вычитано_машиной', 'вычитана', 'не_вычитана'),
    );
    expect(segments.map((s) => s.status)).toEqual([
      'вычитана',
      'вычитано_машиной',
      'требует_внимания',
    ]);
  });

  it('несёт латинский суффикс класса: русские статусы в CSS не пишем', () => {
    const [segment] = readinessSegments(pages('вычитано_машиной'));
    expect(segment.modifier).toBe('machine');
  });
});

describe('readinessLabel', () => {
  it('называет статусы и общее число страниц', () => {
    const label = readinessLabel(pages('вычитана', 'вычитано_машиной', 'не_вычитана'));
    expect(label).toBe('вычитана 1, вычитано машиной 1, всего 3');
  });
});
