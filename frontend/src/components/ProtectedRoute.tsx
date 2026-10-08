import { Navigate, useLocation } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';
import { loginPathFor } from '../utils/returnUrl';
import type { UserRole } from '../types';

interface RequireRoleProps {
  children: React.ReactNode;
  roles: UserRole[];
}

export const RequireRole: React.FC<RequireRoleProps> = ({ children, roles }) => {
  const { isAuthenticated, user } = useAuth();
  const location = useLocation();
  if (!isAuthenticated) {
    return <Navigate to={loginPathFor(location.pathname, location.search)} replace />;
  }
  if (user && !roles.includes(user.role)) return <Navigate to="/" replace />;
  return <>{children}</>;
};
