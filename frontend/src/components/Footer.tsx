import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useLegal, useSite } from '../services/site';
import './Footer.css';

/** «18+» → «для читателей 18 лет и старше»: у знака должно быть внятное имя
 *  для скринридера; знак иного вида читается как есть. */
function ageLabel(rating: string): string {
  const m = /^(\d+)\+$/.exec(rating);
  return m ? `для читателей ${m[1]} лет и старше` : rating;
}

export const Footer: React.FC = () => {
  const currentYear = new Date().getFullYear();
  const location = useLocation();
  // Имя, описание и ссылки подвала — свойство экземпляра (GET /api/site):
  // пустая ссылка — нет строки, а не ссылка в никуда.
  const { siteName, siteDescription, supportUrl, channelUrl, ageRating } = useSite();
  const hasLegal = useLegal().status === 'ready';

  // Форма показывает «Вернуться к чтению» и кладёт этот путь в письмо: без
  // него владелец получит «на 412-й опечатка» и не узнает, в каком томе.
  const here = location.pathname + location.search;
  const feedbackHref = location.pathname.startsWith('/feedback')
    ? '/feedback'
    : `/feedback?from=${encodeURIComponent(here)}`;

  return (
    <footer className="site-footer">
      <div className="footer-content">
        <div className="footer-column">
          <h2 className="footer-heading">{siteName}</h2>
          <p className="footer-about">{siteDescription}</p>
          <Link to="/help#about">О проекте</Link>
          <Link to="/help">Справка</Link>
          {hasLegal && <Link to="/legal">Правовая информация</Link>}
          {supportUrl && (
            <a href={supportUrl} target="_blank" rel="noopener noreferrer">
              Поддержать читальню
            </a>
          )}
        </div>

        <div className="footer-column">
          <h2 className="footer-heading">Каталог</h2>
          <Link to="/">Тома</Link>
          <Link to="/concepts">Указатель</Link>
          <Link to="/collections">Подборки</Link>
          <Link to="/help#offline">{siteName} целиком</Link>
        </div>

        <div className="footer-column">
          <h2 className="footer-heading">Контакты</h2>
          {channelUrl && (
            <a href={channelUrl} target="_blank" rel="noopener noreferrer">
              {/t\.me\//.test(channelUrl) ? 'Телеграм-канал' : 'Канал новостей'}
            </a>
          )}
          <Link to={feedbackHref}>Написать нам</Link>
        </div>
      </div>

      <div className="footer-bottom">
        {/* Маркировка добровольная: 436-ФЗ выводит из-под себя научную продукцию
            и продукцию значительной культурной ценности, а читальня не СМИ.
            Знак ведёт на разъяснение — иначе он немая наклейка.

            Стоит слева намеренно. Справа внизу висит .scroll-dock (position:
            fixed, bottom/inset-inline-end 1.5rem, z-index 90) и накрывал знак
            собой. Отступом справа это не лечится: у дока max-width 16rem и
            ширина зависит от подписи. */}
        {ageRating &&
          (hasLegal ? (
            <Link to="/legal#age" className="age-badge" aria-label={ageLabel(ageRating)}>
              {ageRating}
            </Link>
          ) : (
            <span className="age-badge" aria-label={ageLabel(ageRating)}>
              {ageRating}
            </span>
          ))}
        <p>
          &copy; {currentYear} {siteName}. Материалы открыты для совместной работы.
        </p>
        {/* Неприметная дверь для сотрудников: вход по почте — рядом с
            читательским /join её не место, чтобы «набрал и уже зарегистрирован»
            не соседствовало с формой, где опечатка в адресе — отказ. */}
        <Link to="/login" className="footer-staff-link">
          Вход для редакторов
        </Link>
      </div>
    </footer>
  );
};
