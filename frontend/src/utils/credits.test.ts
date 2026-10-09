import { describe, expect, it } from 'vitest';
import { articleKindLabel, authorsOf, translatorsOf } from './credits';
import type { ArticleCredit } from '../types';

const c = (role: 'author' | 'translator', printed: string, position: number): ArticleCredit => ({
  role,
  printed,
  position,
});

describe('authorsOf', () => {
  it('склеивает авторов по порядку, переводчиков не берёт', () => {
    expect(
      authorsOf([
        c('author', 'П. Юдин', 2),
        c('translator', 'Н. Н.', 3),
        c('author', 'М. Каммари', 1),
      ]),
    ).toBe('М. Каммари, П. Юдин');
  });
  it('пусто без подписи', () => {
    expect(authorsOf(undefined)).toBe('');
    expect(authorsOf([])).toBe('');
  });
});

describe('translatorsOf', () => {
  it('переводчики через запятую', () => {
    expect(translatorsOf([c('author', 'А', 1), c('translator', 'Б', 2)])).toBe('Б');
  });
});

describe('articleKindLabel', () => {
  it('«статья» не подписывается, остальные — по-русски', () => {
    expect(articleKindLabel('статья')).toBe('');
    expect(articleKindLabel('от_редакции')).toBe('от редакции');
    expect(articleKindLabel(null)).toBe('');
  });
});
