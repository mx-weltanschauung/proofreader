import { Link, useLocation } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { ReadLaterShelf } from '../components/ReadLaterShelf';
import { joinPathFor } from '../utils/returnUrl';
import './MyReadLater.css';

export function MyReadLater() {
  const { isAuthenticated, user } = useAuth();
  const location = useLocation();
  useDocumentTitle('Отложено');

  return (
    <div className="read-later-page">
      <h1>Отложено</h1>
      {isAuthenticated && user ? (
        <ReadLaterShelf key={user.id} userId={user.id} />
      ) : (
        <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
          Войти в читальню
        </Link>
      )}
    </div>
  );
}
