import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { authApi } from '../services/api';
import type { User } from '../types';

export const canEdit = (user: User | null): boolean =>
  user?.role === 'editor' || user?.role === 'administrator';

export const isAdmin = (user: User | null): boolean => user?.role === 'administrator';

export const isReader = (user: User | null): boolean => user?.role === 'reader';

// Тот же приём, что mayEdit на сервере (internal/api/collection_handler.go):
// сервер решает по ВЛАДЕЛЬЦУ, не по нику. AuthorNickname — только признак
// «читательская подборка» (пусто — сотрудническая, решает роль) и подпись,
// переживающая удаление учётной записи; право правки живой строки даёт
// исключительно совпадение owner_id. Сравнивать по нику здесь нельзя:
// после задачи 13 (удаление читательской учётной записи) ник освобождается
// и может достаться другому читателю — тот увидел бы кнопку правки на чужой,
// осиротевшей подборке, хотя сервер её не даст. Осиротевшая строка
// (owner_id обнулён) не редактируется никем — это и есть весь смысл
// ник-снимка, а не лазейка для совпавшего ника.
export const canEditCollection = (
  user: User | null,
  collection: { author_nickname: string; owner_id?: number },
): boolean => {
  if (!user) return false;
  if (!collection.author_nickname) return canEdit(user);
  return collection.owner_id != null && collection.owner_id === user.id;
};

// Тот же приём для разборов: mayEditDocument на сервере
// (internal/api/document_access.go) решает по владельцу, а не по нику —
// см. довод у canEditCollection выше, дословно тот же. AuthorNickname
// отличает сотруднический разбор (пуст) от читательского и держит подпись
// «Собрал читатель …», а не право правки; правит читательский разбор
// только его текущий владелец.
export const canEditDocument = (
  user: User | null,
  document: { author_nickname: string; owner_id: number | null },
): boolean => {
  if (!user) return false;
  if (!document.author_nickname) return canEdit(user);
  return document.owner_id != null && document.owner_id === user.id;
};

interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (email: string, password: string) => Promise<void>;
  join: (nickname: string, password: string) => Promise<void>;
  logout: () => void;
  checkAuth: () => Promise<void>;
}

export const useAuth = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: true,

      login: async (email: string, password: string) => {
        const response = await authApi.login(email, password);
        const { user, token } = response.data;

        localStorage.setItem('token', token);
        set({ user, token, isAuthenticated: true, isLoading: false });
      },

      join: async (nickname: string, password: string) => {
        const response = await authApi.join(nickname, password);
        const { user, token } = response.data;

        localStorage.setItem('token', token);
        set({ user, token, isAuthenticated: true, isLoading: false });
      },

      logout: () => {
        localStorage.removeItem('token');
        set({ user: null, token: null, isAuthenticated: false, isLoading: false });
      },

      checkAuth: async () => {
        const token = localStorage.getItem('token');

        if (!token) {
          set({ user: null, token: null, isAuthenticated: false, isLoading: false });
          return;
        }

        try {
          const response = await authApi.me();
          const { token: refreshed, ...user } = response.data;
          // Сервер продлевает читательскую сессию сам, когда токену больше
          // недели; здесь остаётся только сохранить новый.
          const current = refreshed ?? token;
          if (refreshed) localStorage.setItem('token', refreshed);
          set({ user, token: current, isAuthenticated: true, isLoading: false });
        } catch {
          localStorage.removeItem('token');
          set({ user: null, token: null, isAuthenticated: false, isLoading: false });
        }
      },
    }),
    {
      name: 'auth-storage',
      partialize: (state) => ({
        token: state.token,
        user: state.user,
        isAuthenticated: state.isAuthenticated,
      }),
      onRehydrateStorage: () => (state, error) => {
        // После восстановления состояния из localStorage
        if (error) {
          console.error('Failed to rehydrate auth state:', error);
          // Если ошибка - просто завершаем загрузку
          if (state) state.isLoading = false;
          return;
        }

        // Завершаем загрузку после гидрации
        if (state) {
          state.isLoading = false;
          // isAuthenticated, token и user уже восстановлены из localStorage через partialize
          // Если их нет в localStorage, они останутся null/false (начальные значения)
        }
      },
    },
  ),
);
