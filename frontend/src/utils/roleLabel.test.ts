import { describe, it, expect } from 'vitest';
import { roleLabel } from './roleLabel';

describe('roleLabel', () => {
  it('переводит роли на русский', () => {
    expect(roleLabel('administrator')).toBe('администратор');
    expect(roleLabel('editor')).toBe('редактор');
  });
});
