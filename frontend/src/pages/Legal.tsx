import { useEffect } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { scrollBehavior } from '../utils/motion';
import { useLegal } from '../services/site';
import './Legal.css';

/**
 * Правовая информация — текст оператора читальни, а не платформы: каждый
 * экземпляр отвечает за свои утверждения сам. Текст лежит в каталоге
 * экземпляра (instance/legal.html) и отдаётся nginx как есть; файла нет —
 * нет и страницы.
 *
 * HTML, а не markdown: на странице стоят якоря (#age — со знака возрастной
 * маркировки в подвале, #personal-data — с формы обращения) и почтовые
 * ссылки. Файл кладёт тот же человек, что держит сервер, поэтому он
 * доверенный — как и остальное содержимое каталога экземпляра.
 */
export const LegalView: React.FC<{ html: string }> = ({ html }) => {
  const { hash } = useLocation();

  // Тот же приём, что в справке: к якорю после pushState браузер сам не
  // прокручивает, это делает только popstate. Без эффекта ссылка со знака
  // возрастной маркировки открывала бы страницу сверху.
  useEffect(() => {
    if (!hash) return;
    document.getElementById(hash.slice(1))?.scrollIntoView({ behavior: scrollBehavior() });
  }, [hash, html]);

  return <div dangerouslySetInnerHTML={{ __html: html }} />;
};

export const Legal: React.FC = () => {
  const legal = useLegal();
  if (legal.status === 'loading') return null;
  if (legal.status === 'missing') return <Navigate to="/" replace />;
  return <LegalView html={legal.html ?? ''} />;
};
