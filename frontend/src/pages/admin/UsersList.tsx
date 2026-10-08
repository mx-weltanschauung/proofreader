import { useCallback, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { usersApi } from '../../services/api';
import type { AdminUser, UserRole } from '../../types';
import { apiErrorMessage, apiErrorStatus } from '../../utils/apiError';
import { roleLabel } from '../../utils/roleLabel';
import './UsersList.css';

const LAST_ADMIN_MESSAGE = 'Нельзя удалить/разжаловать последнего администратора';

function errorMessage(err: unknown, fallback: string): string {
  if (apiErrorStatus(err) === 409) return LAST_ADMIN_MESSAGE;
  return apiErrorMessage(err, fallback);
}

export const UsersList: React.FC = () => {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  const [newEmail, setNewEmail] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [newRole, setNewRole] = useState<UserRole>('editor');
  const [isCreating, setIsCreating] = useState(false);

  const adminCount = users.filter((u) => u.role === 'administrator').length;

  const loadUsers = useCallback(async () => {
    try {
      const response = await usersApi.list();
      setUsers(response.data || []);
    } catch (err: unknown) {
      setError(errorMessage(err, 'Не удалось загрузить пользователей'));
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    // set-state-in-effect: все setState загрузчика стоят после await, синхронного
    // каскада рендеров нет. Правило требует унести загрузку из эффекта целиком —
    // это переезд на react-query, вынесенный в отдельную задачу. Апстрим-issue
    // facebook/react#34905 висит со статусом Unconfirmed.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadUsers();
  }, [loadUsers]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newEmail || !newPassword) return;

    setIsCreating(true);
    try {
      await usersApi.create({ email: newEmail, password: newPassword, role: newRole });
      toast.success('Пользователь создан');
      setNewEmail('');
      setNewPassword('');
      setNewRole('editor');
      await loadUsers();
    } catch (err: unknown) {
      toast.error(errorMessage(err, 'Не удалось создать пользователя'));
    } finally {
      setIsCreating(false);
    }
  };

  const handleRoleChange = async (user: AdminUser, role: UserRole) => {
    try {
      await usersApi.update(user.id, { role });
      toast.success('Роль обновлена');
      await loadUsers();
    } catch (err: unknown) {
      toast.error(errorMessage(err, 'Не удалось изменить роль'));
    }
  };

  const handleResetPassword = async (user: AdminUser) => {
    const password = window.prompt(`Новый пароль для ${user.email}:`);
    if (!password) return;

    try {
      await usersApi.update(user.id, { password });
      toast.success('Пароль обновлён');
    } catch (err: unknown) {
      toast.error(errorMessage(err, 'Не удалось сбросить пароль'));
    }
  };

  const handleDelete = async (user: AdminUser) => {
    if (!window.confirm(`Удалить пользователя ${user.email}?`)) return;

    try {
      await usersApi.remove(user.id);
      toast.success('Пользователь удалён');
      await loadUsers();
    } catch (err: unknown) {
      toast.error(errorMessage(err, 'Не удалось удалить пользователя'));
    }
  };

  if (isLoading) {
    return <div className="loading-state">Загрузка пользователей…</div>;
  }

  return (
    <div className="users-list-container">
      <h1>Пользователи</h1>

      {error && <div className="error-message">{error}</div>}

      <form className="user-create-form" onSubmit={handleCreate}>
        <h2>Создать пользователя</h2>
        <div className="user-create-form-fields">
          <input
            type="email"
            placeholder="Почта"
            value={newEmail}
            onChange={(e) => setNewEmail(e.target.value)}
            required
          />
          <input
            type="password"
            placeholder="Пароль"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            required
          />
          <select value={newRole} onChange={(e) => setNewRole(e.target.value as UserRole)}>
            <option value="editor">{roleLabel('editor')}</option>
            <option value="administrator">{roleLabel('administrator')}</option>
          </select>
          <button type="submit" disabled={isCreating}>
            {isCreating ? 'Создание...' : 'Создать'}
          </button>
        </div>
      </form>

      <table className="users-table">
        <thead>
          <tr>
            <th>Почта</th>
            <th>Роль</th>
            <th>Действия</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => {
            const isLastAdmin = user.role === 'administrator' && adminCount <= 1;
            return (
              <tr key={user.id}>
                <td>{user.email}</td>
                <td>
                  <select
                    value={user.role}
                    disabled={isLastAdmin}
                    onChange={(e) => handleRoleChange(user, e.target.value as UserRole)}
                  >
                    <option value="editor">{roleLabel('editor')}</option>
                    <option value="administrator">{roleLabel('administrator')}</option>
                  </select>
                </td>
                <td className="users-table-actions">
                  <button type="button" onClick={() => handleResetPassword(user)}>
                    Сбросить пароль
                  </button>
                  <button
                    type="button"
                    className="danger-button"
                    disabled={isLastAdmin}
                    onClick={() => handleDelete(user)}
                  >
                    Удалить
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};
