import { useEffect, useRef, useState } from 'react';
import type { ScanBox } from '../services/api';

export function ProofreadScan({
  url,
  number,
  box,
  refresh,
}: {
  url: string;
  number: number;
  box?: ScanBox;
  refresh: () => void;
}) {
  const [zoom, setZoom] = useState(100);
  const [ratio, setRatio] = useState<number>();
  const [failed, setFailed] = useState(false);
  const marker = useRef<HTMLSpanElement>(null);
  const drag = useRef<{ x: number; y: number; left: number; top: number } | null>(null);
  useEffect(() => {
    marker.current?.scrollIntoView?.({ block: 'nearest', inline: 'center' });
  }, [box, zoom]);
  return (
    <section className="proof-scan">
      <h2>Скан · полоса {number}</h2>
      <div className="proof-tools">
        <button disabled={zoom === 100} onClick={() => setZoom(zoom - 50)}>
          Уменьшить
        </button>
        <span>{zoom}%</span>
        <button disabled={zoom === 400} onClick={() => setZoom(zoom + 50)}>
          Увеличить
        </button>
      </div>
      {failed && (
        <p role="alert">
          Не удалось загрузить скан.{' '}
          <button
            onClick={() => {
              setFailed(false);
              refresh();
            }}
          >
            Обновить ссылку на скан
          </button>
        </p>
      )}
      <div
        className="proof-scan-scroll"
        tabIndex={0}
        aria-label="Область просмотра скана"
        onPointerDown={(e) => {
          if (e.button !== 0 || e.pointerType === 'touch') return;
          const el = e.currentTarget;
          drag.current = { x: e.clientX, y: e.clientY, left: el.scrollLeft, top: el.scrollTop };
          el.setPointerCapture(e.pointerId);
        }}
        onPointerMove={(e) => {
          if (drag.current) {
            e.currentTarget.scrollLeft = drag.current.left + drag.current.x - e.clientX;
            e.currentTarget.scrollTop = drag.current.top + drag.current.y - e.clientY;
          }
        }}
        onLostPointerCapture={() => {
          drag.current = null;
        }}
      >
        <div
          className="proof-image"
          style={{ width: ratio ? `min(${zoom}%, ${(75 * ratio * zoom) / 100}vh)` : zoom + '%' }}
        >
          <img
            src={url}
            alt={'Скан полосы ' + number}
            draggable={false}
            onError={() => setFailed(true)}
            onLoad={(e) => {
              const img = e.currentTarget;
              if (img.naturalHeight) setRatio(img.naturalWidth / img.naturalHeight);
              setFailed(false);
              marker.current?.scrollIntoView?.({ block: 'nearest', inline: 'center' });
            }}
          />
          {box && (
            <span
              ref={marker}
              aria-label="Место находки на скане"
              className="proof-marker"
              style={{
                left: box[0] * 100 + '%',
                top: box[1] * 100 + '%',
                width: box[2] * 100 + '%',
                height: box[3] * 100 + '%',
              }}
            />
          )}
        </div>
      </div>
    </section>
  );
}
