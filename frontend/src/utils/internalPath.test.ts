import { describe, it, expect } from 'vitest';
import { internalPath } from './internalPath';

describe('internalPath', () => {
  it('пропускает путь внутри читальни', () => {
    expect(internalPath('/works/16/pages/412')).toBe('/works/16/pages/412');
    expect(internalPath('/concepts/materializm?letter=М')).toBe('/concepts/materializm?letter=М');
    expect(internalPath('/')).toBe('/');
  });

  it('пропускает слаги с дефисом', () => {
    expect(internalPath('/collections/istoricheskiy-materializm')).toBe(
      '/collections/istoricheskiy-materializm',
    );
  });

  it('отбивает внешний адрес', () => {
    expect(internalPath('https://чужой.сайт/страница')).toBe('');
  });

  it('отбивает protocol-relative адрес', () => {
    expect(internalPath('//чужой.сайт')).toBe('');
  });

  it('отбивает обратный слэш', () => {
    expect(internalPath('/\\чужой.сайт')).toBe('');
  });

  it('отбивает javascript:', () => {
    expect(internalPath('javascript:alert(1)')).toBe('');
  });

  it('отбивает путь без ведущего слэша', () => {
    expect(internalPath('works/16')).toBe('');
  });

  it('отбивает путь с переводом строки', () => {
    expect(internalPath('/works/16\n/pages/1')).toBe('');
  });

  it('отбивает путь длиннее 500 символов', () => {
    expect(internalPath('/' + 'a'.repeat(500))).toBe('');
  });

  it('отбивает пустую строку', () => {
    expect(internalPath('')).toBe('');
    expect(internalPath('   ')).toBe('');
  });
});
