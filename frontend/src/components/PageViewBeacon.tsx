import { useEffect, useRef } from 'react';
import { useLocation } from 'react-router-dom';
import { externalReferrer, sendHit } from '../services/hit';

// Один маячок на смену пути: смена ?from= в потоке чтения — не новый
// просмотр. Источник уходит только с первым просмотром вкладки.
// В dev StrictMode эффект срабатывает дважды — это только разработка.
export const PageViewBeacon: React.FC = () => {
  const { pathname } = useLocation();
  const first = useRef(true);
  useEffect(() => {
    const ref = first.current ? externalReferrer() : '';
    first.current = false;
    sendHit(pathname, ref);
  }, [pathname]);
  return null;
};
