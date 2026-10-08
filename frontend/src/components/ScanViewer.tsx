import React, { useCallback, useEffect, useRef, useState } from 'react';
import InnerImageZoom from 'react-inner-image-zoom';
import 'react-inner-image-zoom/lib/styles.min.css';
import './ScanViewer.css';

interface Props {
  src: string;
  alt: string;
  /** Сообщает наружу, открыт ли полноэкранный просмотр — например, чтобы
   *  страница на время отключила листание стрелками. */
  onOpenChange?: (open: boolean) => void;
}

export const ScanViewer: React.FC<Props> = ({ src, alt, onOpenChange }) => {
  const [full, setFull] = useState(false);
  const openRef = useRef<HTMLButtonElement>(null);

  // Колбэк держим в ref, а не в деп-массивах: родитель вправе передавать
  // новую функцию на каждом рендере, и эффекты ниже не должны от этого
  // перезаводиться. Поведение не зависит от того, мемоизирован ли проп.
  const onOpenChangeRef = useRef(onOpenChange);
  useEffect(() => {
    onOpenChangeRef.current = onOpenChange;
  });

  // Единственная дверь наружу. Помнит последнее сообщённое состояние, поэтому
  // на одно открытие/закрытие уходит ровно одно уведомление — сколько бы
  // источников её ни дёрнуло (close() и страж размонтирования ниже зовут
  // notify(false) оба).
  const notified = useRef(false);
  const notify = useCallback((open: boolean) => {
    if (notified.current === open) return;
    notified.current = open;
    onOpenChangeRef.current?.(open);
  }, []);

  const open = () => {
    setFull(true);
    notify(true);
  };

  // Возврат фокуса — только на закрытии, а не эффектом от `full`: эффект
  // отрабатывает и на первом монтировании (когда `full` уже false), и молча
  // уводил бы фокус на кнопку при каждой загрузке страницы со сканом.
  //
  // useCallback — не оптимизация, а стабильная ссылка для деп-массива ниже:
  // close висит на обработчике Escape, и без мемоизации эффект пришлось бы
  // либо гонять на каждый рендер, либо глушить предупреждение.
  const close = useCallback(() => {
    setFull(false);
    notify(false);
    openRef.current?.focus();
  }, [notify]);

  useEffect(() => {
    if (!full) return;

    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [full, close]);

  // Если сам ScanViewer размонтируется, пока просмотр открыт (у соседней
  // страницы, например, не оказалось превью), close() не вызывается —
  // родитель, не получив onOpenChange(false), навсегда оставил бы листание
  // стрелками выключенным. Деп-массив пуст, так что cleanup срабатывает
  // только при размонтировании; если оверлей к тому моменту уже закрыли,
  // notify молчит.
  useEffect(() => () => notify(false), [notify]);

  return (
    // Обёртка без класса: .scan-viewer-full и .scan-viewer-overlay стилизуют
    // себя сами, ни одно правило не ссылается на родителя — заводить для
    // него класс без стиля незачем.
    <div>
      {/* zoomType="click": при "hover" скан прыгал под указателем от любого
          случайного движения мыши по колонке. */}
      <InnerImageZoom src={src} zoomSrc={src} zoomType="click" zoomScale={1.5} />

      <button
        type="button"
        ref={openRef}
        className="btn btn-secondary btn-sm scan-viewer-full"
        onClick={open}
      >
        Во весь экран
      </button>

      {full && (
        <div
          className="scan-viewer-overlay"
          role="dialog"
          aria-modal="true"
          aria-label={alt}
          onClick={close}
        >
          {/* Клик по самому скану не закрывает: по нему возят, разглядывая. */}
          <img src={src} alt={alt} onClick={(event) => event.stopPropagation()} />
        </div>
      )}
    </div>
  );
};
