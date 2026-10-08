import { useEffect } from 'react';

/**
 * Бесконечная подгрузка по сентинелю: как только пустой узел под последней
 * порцией попадает в кадр, запрашивается следующая.
 *
 * Общий для потока тома и ленты фрагментов понятия. Различие между ними —
 * только запас предзагрузки: поток просит следующее окно за экран до конца,
 * чтобы читатель не упирался в паузу посреди текста, ленте же хватает самого
 * сентинеля.
 *
 * @param rootMargin запас в записи CSS-полей; без него наблюдатель работает
 * по границам кадра.
 */
export function useInfiniteSentinel(
  sentinelRef: React.RefObject<Element>,
  hasMore: boolean,
  loadMore: () => void,
  rootMargin?: string,
): void {
  useEffect(() => {
    const node = sentinelRef.current;
    // Данные кончились — наблюдать не за чем: сентинел, оставшийся в кадре у
    // конца списка, дёргал бы загрузку впустую.
    if (!node || !hasMore) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) loadMore();
      },
      rootMargin ? { rootMargin } : undefined,
    );
    observer.observe(node);

    return () => observer.disconnect();
  }, [sentinelRef, hasMore, loadMore, rootMargin]);
}
