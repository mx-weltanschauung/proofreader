import { describe, it, expect } from 'vitest';
import {
  PAGE_STATUS_LABEL,
  PAGE_STATUS_MODIFIER,
  PAGE_STATUS_ORDER,
  countByStatus,
} from './pageStatus';

describe('pageStatus', () => {
  it('покрывает все семь статусов', () => {
    expect(PAGE_STATUS_ORDER).toHaveLength(7);
    for (const status of PAGE_STATUS_ORDER) {
      expect(PAGE_STATUS_LABEL[status]).toBeTruthy();
      expect(PAGE_STATUS_MODIFIER[status]).toMatch(/^[a-z]+$/);
    }
  });

  it('считает страницы по статусам', () => {
    expect(
      countByStatus([
        { page_number: 1, status: 'не_вычитана' },
        { page_number: 2, status: 'вычитано_машиной' },
        { page_number: 3, status: 'вычитано_машиной' },
      ]),
    ).toEqual({ не_вычитана: 1, вычитано_машиной: 2 });
  });
});
