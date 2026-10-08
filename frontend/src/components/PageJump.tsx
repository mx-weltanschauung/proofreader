import { useEffect, useRef, useState } from 'react';
import './PageJump.css';

export interface PageJumpProps {
  /**
   * Печатная колонцифра видимой полосы — то, что читатель сверит со своей
   * ссылкой. null — полоса ещё не определена (самый верх или низ текста).
   */
  currentLabel: string | null;
  /**
   * Переход по набранному. Резолвится в null, если переход состоялся, и в
   * текст отказа, если такой страницы нет: тогда поле остаётся открытым, а
   * отказ звучит под ним.
   */
  onJump: (input: string) => Promise<string | null>;
  /**
   * Класс кнопки. По умолчанию — тихий, по ширине прежней доли цифрой: в
   * сжатой панели главы запаса по ширине нет, и полноценная кнопка
   * переносила ряд на вторую строку (замер `measure:reading-toolbar`). У
   * панели потока класс свой, соседский.
   */
  className?: string;
  /** Колонцифры римские (передние листы): клавиатура телефона нужна буквенная. */
  roman?: boolean;
}

/** Цель нажатия — поле ввода, где «g» просто буква. */
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.isContentEditable ||
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement
  );
}

/**
 * Номер видимой полосы, который по нажатию становится полем «перейти к
 * странице».
 *
 * Стоит на месте прежней доли прочитанного: панель чтения и так плотная, а
 * долю на глаз по-прежнему показывает полоса прогресса над ней. Куда ведёт
 * переход — решает потребитель (`onJump`), здесь только ввод.
 *
 * На компьютере поле открывает клавиша G. Сравнивается физическая клавиша
 * (`code`), а не буква: в русской раскладке та же клавиша печатает «п», и
 * читатель, набирающий по-русски, иначе её бы не нашёл. «/» занята поиском.
 */
export const PageJump: React.FC<PageJumpProps> = ({
  currentLabel,
  onJump,
  className = 'page-jump-quiet',
  roman = false,
}) => {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState('');
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  // Фокус возвращается на кнопку только после явной отмены (Escape): при
  // уходе фокуса читатель сам выбрал, куда его деть, а после перехода
  // кнопка оказывается на новом месте текста или вовсе на другом экране.
  const returnFocusRef = useRef(false);

  const openField = () => {
    setValue('');
    setError('');
    setOpen(true);
  };

  const close = () => {
    setOpen(false);
    setError('');
  };

  useEffect(() => {
    if (open) {
      inputRef.current?.focus();
    } else if (returnFocusRef.current) {
      returnFocusRef.current = false;
      buttonRef.current?.focus();
    }
  }, [open]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.code !== 'KeyG') return;
      if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return;
      if (isTypingTarget(event.target)) return;
      // Без этого буква, нажатая на кнопке, доехала бы до только что
      // открытого поля и встала в него первым знаком.
      event.preventDefault();
      setValue('');
      setError('');
      setOpen(true);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (pending || value.trim() === '') return;
    setPending(true);
    try {
      const failure = await onJump(value);
      if (failure) {
        setError(failure);
      } else {
        close();
      }
    } finally {
      setPending(false);
    }
  };

  if (!open) {
    return (
      <button
        ref={buttonRef}
        type="button"
        className={`${className} page-jump-trigger`}
        aria-label={
          currentLabel ? `Перейти к странице (сейчас ${currentLabel})` : 'Перейти к странице'
        }
        aria-keyshortcuts="G"
        onClick={openField}
      >
        с. {currentLabel ?? '…'}
      </button>
    );
  }

  return (
    <form className="page-jump" onSubmit={(event) => void submit(event)}>
      <input
        ref={inputRef}
        className="page-jump-input"
        aria-label="Номер страницы"
        aria-invalid={error !== '' || undefined}
        aria-describedby={error ? 'page-jump-error' : undefined}
        inputMode={roman ? 'text' : 'numeric'}
        enterKeyHint="go"
        autoComplete="off"
        placeholder={currentLabel ?? ''}
        value={value}
        // readOnly, а не disabled: отключённое поле Chrome лишает фокуса, и
        // после отказа Escape уходил мимо поля, а набранное дописывалось к
        // прежнему номеру. jsdom фокуса не снимает — тест этого не видел.
        readOnly={pending}
        onChange={(event) => {
          setValue(event.target.value);
          setError('');
        }}
        onKeyDown={(event) => {
          if (event.key !== 'Escape') return;
          // Escape здесь — отмена ввода, и только: у панели главы он же
          // выходит из режима чтения, у потока — из самого потока, и оба
          // слушают окно. Дальше поля он уйти не должен.
          event.stopPropagation();
          returnFocusRef.current = true;
          close();
        }}
        onBlur={() => {
          if (!pending) close();
        }}
      />
      {error && (
        <p id="page-jump-error" className="page-jump-error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
};
