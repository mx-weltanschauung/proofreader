import { describe, it, expect, beforeEach, vi } from 'vitest';
import { readVolumeView, writeVolumeView } from './volumeViewPref';

describe('volumeViewPref', () => {
  beforeEach(() => localStorage.clear());

  it('по умолчанию — корешки', () => {
    expect(readVolumeView()).toBe('spines');
  });

  it('запоминает выбор', () => {
    writeVolumeView('list');
    expect(readVolumeView()).toBe('list');
  });

  // localStorage открыт на запись кому угодно: мусор в ключе не должен
  // ронять страницу собрания.
  it('переживает мусор в хранилище', () => {
    localStorage.setItem('volume-view', '{"нет":"такого"}');
    expect(readVolumeView()).toBe('spines');
  });

  // В приватном окне и во вложенном контексте с запретом на хранилище
  // getItem/setItem бросают. readVolumeView вызывается из инициализатора
  // состояния во время рендера — исключение оттуда оставило бы страницу
  // собрания пустой.
  it('переживает запрет на чтение хранилища', () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(readVolumeView()).toBe('spines');
    spy.mockRestore();
  });

  it('переживает запрет на запись в хранилище', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => writeVolumeView('list')).not.toThrow();
    spy.mockRestore();
  });
});
