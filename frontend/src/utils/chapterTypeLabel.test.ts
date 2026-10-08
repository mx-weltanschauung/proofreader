import { describe, it, expect } from 'vitest';
import { chapterTypeLabel } from './chapterTypeLabel';

describe('chapterTypeLabel', () => {
  it('переводит известные типы', () => {
    expect(chapterTypeLabel('chapter')).toBe('глава');
    expect(chapterTypeLabel('preface')).toBe('предисловие');
    expect(chapterTypeLabel('appendix')).toBe('приложение');
  });

  // В базе может лежать тип, которого нет в списке формы: показать его как
  // есть честнее, чем подставить «глава».
  it('незнакомый тип отдаёт как есть', () => {
    expect(chapterTypeLabel('letter')).toBe('letter');
  });
});
