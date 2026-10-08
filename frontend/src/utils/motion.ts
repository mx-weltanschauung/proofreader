/**
 * Просил ли читатель ограничить движение в системных настройках.
 *
 * CSS-правило `scroll-behavior: auto` под `@media (prefers-reduced-motion:
 * reduce)` на опцию `behavior` в `scrollTo`/`scrollIntoView` не влияет: JS
 * задаёт её сам, и без этой проверки настройка просто игнорируется. Отсюда
 * общая функция — у каждого места прокрутки был свой matchMedia.
 */
export function prefersReducedMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** Значение `behavior` для прокрутки с учётом этой настройки. */
export function scrollBehavior(): ScrollBehavior {
  return prefersReducedMotion() ? 'auto' : 'smooth';
}
