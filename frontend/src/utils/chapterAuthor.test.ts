import { describe, it, expect } from 'vitest';
import { splitAuthor } from './chapterAuthor';

describe('splitAuthor', () => {
  it('отделяет одного автора', () => {
    expect(splitAuthor('К. Маркс. Временный устав Товарищества')).toEqual({
      author: 'К. Маркс',
      title: 'Временный устав Товарищества',
    });
  });

  it('отделяет двух авторов', () => {
    expect(splitAuthor('К. Маркс и Ф. Энгельс. Заявление в редакцию')).toEqual({
      author: 'К. Маркс и Ф. Энгельс',
      title: 'Заявление в редакцию',
    });
  });

  it('понимает два инициала', () => {
    expect(splitAuthor('Г. В. Плеханов. Очерки по истории материализма')).toEqual({
      author: 'Г. В. Плеханов',
      title: 'Очерки по истории материализма',
    });
  });

  it('оставляет заголовок без автора как есть', () => {
    expect(splitAuthor('Приложения')).toEqual({ author: null, title: 'Приложения' });
  });

  // Номерная подглава не должна принять свой номер за инициалы.
  it('не находит автора в «I. Редактору газеты»', () => {
    expect(splitAuthor('I. Редактору газеты «Commonwealth»').author).toBeNull();
  });
});
