/** Значки проигрывателя: свои SVG, цвет — currentColor, подпись — у кнопки
 *  (aria-label), сам значок экранному диктору не виден. */

interface IconProps {
  size?: number;
}

function Svg({ size = 20, children }: IconProps & { children: React.ReactNode }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      {children}
    </svg>
  );
}

export function PlayIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <path d="M8 5.5v13l11-6.5z" fill="currentColor" />
    </Svg>
  );
}

export function PauseIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <path d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z" fill="currentColor" />
    </Svg>
  );
}

export function PrevIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <path d="M6 5h2v14H6zM19 5.5v13L9.5 12z" fill="currentColor" />
    </Svg>
  );
}

export function NextIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <path d="M16 5h2v14h-2zM5 5.5v13L14.5 12z" fill="currentColor" />
    </Svg>
  );
}

export function DownloadIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <path
        d="M12 4v10m0 0l-4.5-4.5M12 14l4.5-4.5M5 19h14"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}

export function CloseIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <path
        d="M6 6l12 12M18 6L6 18"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Крутилка загрузки; вращение — в CSS (.icon-spin), под prefers-reduced-motion стоит. */
export function SpinnerIcon(p: IconProps) {
  return (
    <Svg {...p}>
      <g className="icon-spin">
        <path
          d="M12 3a9 9 0 1 0 9 9"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
        />
      </g>
    </Svg>
  );
}
