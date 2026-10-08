import { describe, it, expect } from 'vitest';
import { apiErrorMessage, apiErrorStatus } from './apiError';

describe('apiErrorMessage', () => {
  it('берёт message из тела ответа', () => {
    const err = { response: { data: { message: 'Страница занята' } } };
    expect(apiErrorMessage(err, 'запасной')).toBe('Страница занята');
  });

  it('отдаёт запасной текст, когда message пустой', () => {
    const err = { response: { data: { message: '' } } };
    expect(apiErrorMessage(err, 'запасной')).toBe('запасной');
  });

  it('отдаёт запасной текст, когда ответа нет вовсе', () => {
    expect(apiErrorMessage(new Error('сеть'), 'запасной')).toBe('запасной');
  });

  // Ошибка приходит из axios и по типам это unknown: перебрать её формы надо
  // без падения, иначе обработчик ошибки сам станет источником ошибки.
  it('не падает на мусоре вместо ошибки', () => {
    expect(apiErrorMessage(null, 'запасной')).toBe('запасной');
    expect(apiErrorMessage(undefined, 'запасной')).toBe('запасной');
    expect(apiErrorMessage('строка', 'запасной')).toBe('запасной');
    expect(apiErrorMessage({ response: null }, 'запасной')).toBe('запасной');
    expect(apiErrorMessage({ response: { data: 42 } }, 'запасной')).toBe('запасной');
  });

  it('игнорирует message нестрокового типа', () => {
    expect(apiErrorMessage({ response: { data: { message: 42 } } }, 'запасной')).toBe('запасной');
  });
});

describe('apiErrorStatus', () => {
  it('отдаёт код ответа', () => {
    expect(apiErrorStatus({ response: { status: 409 } })).toBe(409);
  });

  it('отдаёт undefined, когда кода нет', () => {
    expect(apiErrorStatus(new Error('сеть'))).toBeUndefined();
    expect(apiErrorStatus(null)).toBeUndefined();
    expect(apiErrorStatus({ response: { status: 'нет' } })).toBeUndefined();
  });
});
