import React, { useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';
import { useReadingPrefs } from '../contexts/readingPrefsContext';
import type {
  ReadingPrefs,
  ThemeSetting,
  FontFamilySetting,
  LineHeightSetting,
  MeasureSetting,
  SpacingSetting,
  ParagraphSetting,
  AlignSetting,
} from '../contexts/readingPrefs';
import { FONT_SIZE_MIN, FONT_SIZE_MAX } from '../contexts/readingPrefs';
import { useDrawerOverlap } from './useDrawerOverlap';
import { useDrawerChrome } from './useDrawerChrome';
import { FeatureHint } from './FeatureHint';
import './ReadingSettings.css';

const THEMES: { key: ThemeSetting; label: string }[] = [
  { key: 'light', label: 'Светлая' },
  { key: 'sepia', label: 'Сепия' },
  { key: 'gray-dim', label: 'Приглушённая' },
  { key: 'dark', label: 'Тёмная' },
  { key: 'oled', label: 'OLED' },
  { key: 'high-contrast', label: 'Контрастная' },
  { key: 'auto', label: 'Как в системе' },
];

const FONTS: { key: FontFamilySetting; label: string; note: string }[] = [
  { key: 'literata', label: 'Literata', note: 'книжная антиква' },
  { key: 'fira', label: 'Fira Sans', note: 'гротеск' },
  { key: 'andika', label: 'Andika', note: 'максимально различимые буквы' },
];

const LINE_HEIGHTS: { key: LineHeightSetting; label: string }[] = [
  { key: 'tight', label: 'Плотный' },
  { key: 'normal', label: 'Обычный' },
  { key: 'loose', label: 'Свободный' },
  { key: 'x-loose', label: 'Очень свободный' },
];

const MEASURES: { key: MeasureSetting; label: string }[] = [
  { key: 'narrow', label: '55 знаков' },
  { key: 'normal', label: '65 знаков' },
  { key: 'wide', label: '75 знаков' },
  { key: 'full', label: 'Во всю ширину' },
];

const SPACINGS: { key: SpacingSetting; label: string }[] = [
  { key: 'normal', label: 'Обычные' },
  { key: 'roomy', label: 'Просторные' },
  { key: 'x-roomy', label: 'Очень просторные' },
];

const PARAGRAPHS: { key: ParagraphSetting; label: string }[] = [
  { key: 'indent', label: 'Красная строка' },
  { key: 'spaced', label: 'Отбивка' },
];

/** Булева настройка в том же виде, что и остальные ряды выбора. */
const PAGE_NUMBERS: { key: 'on' | 'off'; label: string }[] = [
  { key: 'on', label: 'Показывать' },
  { key: 'off', label: 'Скрывать' },
];

const ALIGNS: { key: AlignSetting; label: string }[] = [
  { key: 'left', label: 'По левому краю' },
  { key: 'justify', label: 'По формату' },
];

/** A labelled row of mutually exclusive choices, exposed as an ARIA radiogroup. */
function Choice<K extends string>({
  label,
  options,
  value,
  onChange,
  variant,
}: {
  label: string;
  options: { key: K; label: string; note?: string }[];
  value: K;
  onChange: (key: K) => void;
  variant?: 'swatch' | 'font';
}) {
  const id = useId();
  return (
    <div className="rs-group">
      <div className="rs-label" id={id}>
        {label}
      </div>
      <div
        className={`rs-choices${variant ? ` rs-choices--${variant}` : ''}`}
        role="radiogroup"
        aria-labelledby={id}
      >
        {options.map((o) => (
          <button
            key={o.key}
            type="button"
            role="radio"
            aria-checked={value === o.key}
            // Without this the accessible name would swallow the note as well
            // ("Fira Sans гротеск"), which is not what anyone would call it.
            aria-label={o.label}
            className={`rs-choice${value === o.key ? ' is-active' : ''}`}
            data-value={o.key}
            onClick={() => onChange(o.key)}
          >
            <span className="rs-choice-label">{o.label}</span>
            {o.note && <span className="rs-choice-note">{o.note}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

export const ReadingSettings: React.FC = () => {
  const { prefs, setPref, reset } = useReadingPrefs();
  const [open, setOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useDrawerOverlap(open);

  const close = () => {
    setOpen(false);
    toggleRef.current?.focus();
  };

  useDrawerChrome({
    open,
    panelRef,
    toggleRef,
    onEscape: close,
    onOutsideClick: () => setOpen(false),
  });

  const setSize = (delta: number) =>
    setPref('fontSize', Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, prefs.fontSize + delta)));

  const set =
    <K extends keyof ReadingPrefs>(key: K) =>
    (value: ReadingPrefs[K]) =>
      setPref(key, value);

  return (
    <div className="reading-settings">
      <button
        ref={toggleRef}
        type="button"
        className="reading-settings-toggle"
        aria-label="Настройки чтения"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span aria-hidden="true">Aa</span>
      </button>
      {/* Кнопка смонтирована дважды — здесь и в панели потокового чтения,
          где шапка сайта скрыта через display:none. Разводить копии руками
          не нужно: право показаться координатор выдаёт экземпляру, чей
          якорь виден, а не идентификатору (см. HintInstance). */}
      <FeatureHint id="reading-settings" anchorRef={toggleRef} />

      {/* Portalled to the body on purpose. The header is position:sticky with
          z-index:100, which makes it a stacking context; a drawer rendered
          inside it would resolve its own z-index within that context and end
          up painted under .reading-progress-bar (fixed, z-index 1000, in the
          root context). */}
      {open &&
        createPortal(
          <>
            <div className="rs-scrim" aria-hidden="true" />
            <div
              ref={panelRef}
              className="rs-panel"
              role="dialog"
              aria-modal="true"
              aria-labelledby={titleId}
            >
              <div className="rs-head">
                <h2 className="rs-title" id={titleId}>
                  Как читать
                </h2>
                <button type="button" className="rs-close" aria-label="Закрыть" onClick={close}>
                  <span aria-hidden="true">✕</span>
                </button>
              </div>

              <div className="rs-body">
                <Choice
                  label="Тема"
                  variant="swatch"
                  options={THEMES}
                  value={prefs.theme}
                  onChange={set('theme')}
                />
                <Choice
                  label="Шрифт"
                  variant="font"
                  options={FONTS}
                  value={prefs.fontFamily}
                  onChange={set('fontFamily')}
                />

                <div className="rs-group">
                  <div className="rs-label" id={`${titleId}-size`}>
                    Кегль
                  </div>
                  <div className="rs-stepper" role="group" aria-labelledby={`${titleId}-size`}>
                    <button
                      type="button"
                      className="rs-step"
                      aria-label="Уменьшить кегль"
                      disabled={prefs.fontSize <= FONT_SIZE_MIN}
                      onClick={() => setSize(-1)}
                    >
                      −
                    </button>
                    <output className="rs-step-value">{prefs.fontSize}px</output>
                    <button
                      type="button"
                      className="rs-step"
                      aria-label="Увеличить кегль"
                      disabled={prefs.fontSize >= FONT_SIZE_MAX}
                      onClick={() => setSize(1)}
                    >
                      +
                    </button>
                  </div>
                </div>

                <Choice
                  label="Интерлиньяж"
                  options={LINE_HEIGHTS}
                  value={prefs.lineHeight}
                  onChange={set('lineHeight')}
                />
                <Choice
                  label="Ширина колонки"
                  options={MEASURES}
                  value={prefs.measure}
                  onChange={set('measure')}
                />
                <Choice
                  label="Интервалы"
                  options={SPACINGS}
                  value={prefs.spacing}
                  onChange={set('spacing')}
                />
                <Choice
                  label="Абзацы"
                  options={PARAGRAPHS}
                  value={prefs.paragraph}
                  onChange={set('paragraph')}
                />
                <Choice
                  label="Выключка"
                  options={ALIGNS}
                  value={prefs.align}
                  onChange={set('align')}
                />
                {/* Та же настройка, что и у кнопок «Номера страниц» в панели
                    главы и «№» в панели потока: они переключают её же, а не
                    своё состояние экрана. Здесь она названа полностью — в
                    панелях на подпись места нет. */}
                <Choice
                  label="Номера страниц"
                  options={PAGE_NUMBERS}
                  value={prefs.pageNumbers ? 'on' : 'off'}
                  onChange={(key) => setPref('pageNumbers', key === 'on')}
                />
              </div>

              <div className="rs-foot">
                <Link className="rs-help-link" to="/help" onClick={close}>
                  Возможности читальни
                </Link>
                <button type="button" className="rs-reset" onClick={reset}>
                  Сбросить всё
                </button>
              </div>
            </div>
          </>,
          document.body,
        )}
    </div>
  );
};
