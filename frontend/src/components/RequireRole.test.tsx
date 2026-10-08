import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useSearchParams } from 'react-router-dom';
import { RequireRole } from './ProtectedRoute';
import { useAuth } from '../hooks/useAuth';

function LoginProbe() {
  const [params] = useSearchParams();
  return (
    <>
      <div>login page</div>
      <div data-testid="next">{params.get('next') ?? ''}</div>
    </>
  );
}

function renderAt(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/login" element={<LoginProbe />} />
        <Route path="/" element={<div>home</div>} />
        <Route
          path="/admin"
          element={
            <RequireRole roles={['administrator']}>
              <div>admin area</div>
            </RequireRole>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

describe('RequireRole', () => {
  beforeEach(() =>
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false }),
  );

  it('redirects guest to login', () => {
    renderAt('/admin');
    expect(screen.getByText('login page')).toBeInTheDocument();
  });
  it('кладёт исходный путь в next при отправке гостя на логин', () => {
    renderAt('/admin');
    expect(screen.getByTestId('next').textContent).toBe('/admin');
  });
  it('кладёт в next путь вместе с query-строкой', () => {
    renderAt('/admin?tab=users');
    expect(screen.getByTestId('next').textContent).toBe('/admin?tab=users');
  });
  it('redirects editor away from admin route', () => {
    useAuth.setState({
      user: { id: 1, email: 'e', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderAt('/admin');
    expect(screen.getByText('home')).toBeInTheDocument();
  });
  it('lets admin in', () => {
    useAuth.setState({
      user: { id: 1, email: 'a', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderAt('/admin');
    expect(screen.getByText('admin area')).toBeInTheDocument();
  });
});
