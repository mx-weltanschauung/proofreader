import { useEffect, useState } from 'react';
import { readToasterColors, type ToasterColors } from '../utils/themeColor';

/**
 * Цвета тоста, читаемые из активной темы и живые к её смене.
 *
 * data-theme на <html> проставляет ReadingPreferencesContext в своём
 * passive-эффекте — в родителе. React коммитит рендер целиком раньше, чем
 * срабатывает хоть один эффект, а сами эффекты идут снизу вверх: рендер
 * этого хука (и любое чтение цвета прямо в нём) всегда проходит раньше, чем
 * родитель успевает переставить атрибут. Подписки на контекст самой темы
 * недостаточно — «где-то потом случился повторный рендер» не то же самое,
 * что «повторный рендер после того, как атрибут действительно поменялся»;
 * без случайного стороннего повторного рендера тост так и остаётся с
 * цветом предыдущей темы.
 *
 * Поэтому цвета не читаются на рендер: они лежат в состоянии, и
 * MutationObserver следит за самим атрибутом data-theme и перечитывает
 * токены только тогда, когда тот действительно изменился — это верно вне
 * зависимости от порядка эффектов между компонентами.
 */
export function useToasterColors(): ToasterColors {
  const [colors, setColors] = useState(readToasterColors);

  useEffect(() => {
    const update = () => setColors(readToasterColors());
    update(); // на случай, если data-theme уже стоял верно к моменту подписки
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    });
    return () => observer.disconnect();
  }, []);

  return colors;
}
