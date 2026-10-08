import { describe, it, expect } from 'vitest';
import { loginPathFor, joinPathFor, safeReturnPath } from './returnUrl';

const TARGET = '/works/6/pages/493/edit';

describe('safeReturnPath', () => {
  it('пустое значение уводит на главную', () => {
    expect(safeReturnPath(null)).toBe('/');
    expect(safeReturnPath(undefined)).toBe('/');
    expect(safeReturnPath('')).toBe('/');
  });

  it('внешние адреса и чужие схемы уводят на главную', () => {
    expect(safeReturnPath('https://evil.example')).toBe('/');
    expect(safeReturnPath('javascript:alert(1)')).toBe('/');
    expect(safeReturnPath('works/6')).toBe('/');
  });

  it('протокол-относительные адреса уводят на главную', () => {
    expect(safeReturnPath('//evil.example')).toBe('/');
    expect(safeReturnPath('/\\evil.example')).toBe('/');
  });

  it('сам логин уводит на главную, чтобы не было петли', () => {
    expect(safeReturnPath('/login')).toBe('/');
    expect(safeReturnPath('/login?next=/x')).toBe('/');
    expect(safeReturnPath('/login#anchor')).toBe('/');
  });

  it('сама дверь читателя уводит на главную, чтобы не было петли', () => {
    expect(safeReturnPath('/join')).toBe('/');
    expect(safeReturnPath('/join?next=/x')).toBe('/');
    expect(safeReturnPath('/JOIN')).toBe('/');
    expect(safeReturnPath('/join/')).toBe('/');
  });

  it('похожий, но другой путь проходит', () => {
    expect(safeReturnPath('/logins')).toBe('/logins');
  });

  it('внутренний путь возвращается как есть', () => {
    expect(safeReturnPath(TARGET)).toBe(TARGET);
    expect(safeReturnPath('/works?page=2#top')).toBe('/works?page=2#top');
    expect(safeReturnPath('/')).toBe('/');
  });

  it('управляющие символы не дают замаскировать протокол-относительный адрес', () => {
    expect(safeReturnPath('/\t/evil.example')).toBe('/');
    expect(safeReturnPath('/\n/evil.example')).toBe('/');
    expect(safeReturnPath('/\r/evil.example')).toBe('/');
    expect(safeReturnPath('/\t\\evil.example')).toBe('/');
    expect(safeReturnPath('/works\t/6')).toBe('/');
  });

  it('регистр и хвостовой слэш не дают обойти проверку на /login', () => {
    expect(safeReturnPath('/LOGIN')).toBe('/');
    expect(safeReturnPath('/Login')).toBe('/');
    expect(safeReturnPath('/login/')).toBe('/');
    expect(safeReturnPath('/login//')).toBe('/');
  });

  it('percent-encoding не даёт обойти проверку на /login', () => {
    expect(safeReturnPath('/%6Cogin')).toBe('/');
  });

  it('невалидный percent-encoding считается подозрительным и уводит на главную', () => {
    expect(safeReturnPath('/%zz')).toBe('/');
  });

  it('похожий, но другой путь с учётом нормализации всё равно проходит', () => {
    expect(safeReturnPath('/logins')).toBe('/logins');
  });
});

describe('loginPathFor', () => {
  it('кладёт путь в next с процентным кодированием', () => {
    expect(loginPathFor(TARGET)).toBe(`/login?next=${encodeURIComponent(TARGET)}`);
  });

  it('кодирует путь вместе с query', () => {
    expect(loginPathFor('/works', '?page=2')).toBe(
      `/login?next=${encodeURIComponent('/works?page=2')}`,
    );
  });

  it('с главной не добавляет next — возвращать некуда', () => {
    expect(loginPathFor('/')).toBe('/login');
    expect(loginPathFor('/', '')).toBe('/login');
  });

  it('уже на логине — сохраняет существующий next', () => {
    expect(loginPathFor('/login', '?next=%2Fworks%2F6')).toBe('/login?next=%2Fworks%2F6');
    expect(loginPathFor('/login')).toBe('/login');
  });
});

describe('joinPathFor', () => {
  it('кладёт путь в next с процентным кодированием', () => {
    expect(joinPathFor(TARGET)).toBe(`/join?next=${encodeURIComponent(TARGET)}`);
  });

  it('кодирует путь вместе с query', () => {
    expect(joinPathFor('/works', '?page=2')).toBe(
      `/join?next=${encodeURIComponent('/works?page=2')}`,
    );
  });

  it('с главной не добавляет next — возвращать некуда', () => {
    expect(joinPathFor('/')).toBe('/join');
    expect(joinPathFor('/', '')).toBe('/join');
  });

  it('уже на двери читателя — сохраняет существующий next', () => {
    expect(joinPathFor('/join', '?next=%2Fworks%2F6')).toBe('/join?next=%2Fworks%2F6');
    expect(joinPathFor('/join')).toBe('/join');
  });
});
