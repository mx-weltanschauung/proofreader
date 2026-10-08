import { describe, it, expect } from 'vitest';

import type { WorkStatus } from '../types';
import { workStatusLabel } from './workStatusLabel';

describe('workStatusLabel', () => {
  it('переводит известные статусы', () => {
    expect(workStatusLabel('draft')).toBe('Черновик');
    expect(workStatusLabel('in_progress')).toBe('В работе');
    expect(workStatusLabel('completed')).toBe('Завершена');
    expect(workStatusLabel('archived')).toBe('В архиве');
  });

  // В базе может лежать статус, которого нет в списке формы: показать его
  // как есть честнее, чем подставить «Черновик».
  it('незнакомый статус отдаёт как есть', () => {
    expect(workStatusLabel('paused' as WorkStatus)).toBe('paused');
  });
});
