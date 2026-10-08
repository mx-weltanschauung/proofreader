import { describe, it, expect, vi, beforeEach } from 'vitest';
import { canEdit, canEditCollection, canEditDocument, isAdmin, isReader } from './useAuth';

vi.mock('../services/api', () => ({
  authApi: {
    login: vi.fn(),
    join: vi.fn(),
    me: vi.fn(),
  },
}));

import { authApi } from '../services/api';
import { useAuth } from './useAuth';

describe('role helpers', () => {
  it('canEdit true for editor and admin, false for guest and reader', () => {
    expect(canEdit({ id: 1, email: 'e', role: 'editor' })).toBe(true);
    expect(canEdit({ id: 1, email: 'a', role: 'administrator' })).toBe(true);
    expect(canEdit({ id: 1, email: '', nickname: 'ник', role: 'reader' })).toBe(false);
    expect(canEdit(null)).toBe(false);
  });
  it('isAdmin only for administrator', () => {
    expect(isAdmin({ id: 1, email: 'a', role: 'administrator' })).toBe(true);
    expect(isAdmin({ id: 1, email: 'e', role: 'editor' })).toBe(false);
    expect(isAdmin(null)).toBe(false);
  });
  it('isReader only for reader', () => {
    expect(isReader({ id: 1, email: '', nickname: 'ник', role: 'reader' })).toBe(true);
    expect(isReader({ id: 1, email: 'e', role: 'editor' })).toBe(false);
    expect(isReader(null)).toBe(false);
  });
});

// Задача 13 (удаление читательской учётной записи) освобождает ник, и его
// может занять другой читатель — canEditCollection/canEditDocument обязаны
// решать по владельцу (owner_id), а не по нику, иначе новый читатель с тем
// же освободившимся ником увидел бы кнопку правки на чужой, осиротевшей
// вещи. Сервер (mayEdit/mayEditDocument) решает именно так — сверено
// дословно с internal/api/collection_handler.go и document_access.go.
describe('canEditCollection — по владельцу, не по нику', () => {
  it('сотрудническая подборка (пустой ник) решается ролью', () => {
    expect(
      canEditCollection({ id: 1, email: 'a@b.c', role: 'editor' }, { author_nickname: '' }),
    ).toBe(true);
    expect(
      canEditCollection(
        { id: 1, email: '', nickname: 'вася', role: 'reader' },
        { author_nickname: '' },
      ),
    ).toBe(false);
  });

  it('владелец правит свою подборку', () => {
    expect(
      canEditCollection(
        { id: 7, email: '', nickname: 'вася', role: 'reader' },
        { author_nickname: 'вася', owner_id: 7 },
      ),
    ).toBe(true);
  });

  it('совпадение ника без совпадения владельца не даёт правки — освободившийся ник', () => {
    // Подборку собрал читатель «вася» (id 7), учётку удалили — owner_id
    // обнулён (ON DELETE SET NULL), подпись «вася» осталась снимком. Новый
    // читатель занял тот же ник под другим id (2).
    expect(
      canEditCollection(
        { id: 2, email: '', nickname: 'вася', role: 'reader' },
        { author_nickname: 'вася', owner_id: undefined },
      ),
    ).toBe(false);
  });

  it('чужой владелец (другой id, тот же ник теоретически не нужен) не правит', () => {
    expect(
      canEditCollection(
        { id: 2, email: '', nickname: 'петя', role: 'reader' },
        { author_nickname: 'вася', owner_id: 7 },
      ),
    ).toBe(false);
  });
});

describe('canEditDocument — по владельцу, не по нику', () => {
  it('сотруднический разбор (пустой ник) решается ролью', () => {
    expect(
      canEditDocument(
        { id: 1, email: 'a@b.c', role: 'editor' },
        { author_nickname: '', owner_id: null },
      ),
    ).toBe(true);
    expect(
      canEditDocument(
        { id: 1, email: '', nickname: 'вася', role: 'reader' },
        { author_nickname: '', owner_id: null },
      ),
    ).toBe(false);
  });

  it('владелец правит свой разбор', () => {
    expect(
      canEditDocument(
        { id: 7, email: '', nickname: 'вася', role: 'reader' },
        { author_nickname: 'вася', owner_id: 7 },
      ),
    ).toBe(true);
  });

  it('осиротевший разбор (owner_id null) не редактируется даже тем же ником', () => {
    // Ровно случай задачи 13: учётку автора удалили, ник «вася» освободился
    // и достался другому читателю с id 2 — правки быть не должно.
    expect(
      canEditDocument(
        { id: 2, email: '', nickname: 'вася', role: 'reader' },
        { author_nickname: 'вася', owner_id: null },
      ),
    ).toBe(false);
  });

  it('чужой владелец не правит', () => {
    expect(
      canEditDocument(
        { id: 2, email: '', nickname: 'петя', role: 'reader' },
        { author_nickname: 'вася', owner_id: 7 },
      ),
    ).toBe(false);
  });
});

describe('checkAuth — скользящий срок читателя', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  // Сервер продлевает читательскую сессию сам и присылает свежий токен
  // отдельным полем `token` в ответе /auth/me, когда текущему больше недели.
  // Если checkAuth перестанет его забирать (вернуть как раньше — токен из
  // хранилища), это поле молча потеряется, и через три месяца читатель
  // просто перестанет входить.
  it('сохраняет продлённый токен из /auth/me и не кладёт его в user', async () => {
    localStorage.setItem('token', 'old-token');
    vi.mocked(authApi.me).mockResolvedValue({
      data: { id: 1, email: '', nickname: 'ник', role: 'reader', token: 'new-token' },
    } as never);

    await useAuth.getState().checkAuth();

    expect(localStorage.getItem('token')).toBe('new-token');
    expect(useAuth.getState().token).toBe('new-token');
    expect(useAuth.getState().user).toEqual({
      id: 1,
      email: '',
      nickname: 'ник',
      role: 'reader',
    });
  });

  it('без поля token в ответе оставляет токен из хранилища как есть', async () => {
    localStorage.setItem('token', 'old-token');
    vi.mocked(authApi.me).mockResolvedValue({
      data: { id: 2, email: 'e@x.io', role: 'editor' },
    } as never);

    await useAuth.getState().checkAuth();

    expect(localStorage.getItem('token')).toBe('old-token');
    expect(useAuth.getState().token).toBe('old-token');
    expect(useAuth.getState().user).toEqual({ id: 2, email: 'e@x.io', role: 'editor' });
  });
});
