import { useMemo } from 'react';
import { diffLines, diffWords, diffWordsWithSpace } from 'diff';
import type { Change } from 'diff';
import './MarkdownDiff.css';

export interface MarkdownDiffProps {
  before: string;
  after: string;
}

// jsdiff's tokenizer для diffWords/diffWordsWithSpace распознаёт «словом»
// только латиницу (extendedWordChars в jsdiff — латинские буквы и цифры),
// кириллица в него не входит. Без intlSegmenter «Пролетарiат»/«Пролетариат»
// диффятся посимвольно (единственная замена — «i»/«и»), и diffWordsWithSpace
// вдобавок этот option молча игнорирует — его тут же читает только diffWords.
// Русский сегментатор — то, что делает диф пословным на кириллице.
//
// Intl.Segmenter — не универсальная данность: в Firefox он появился в версии
// 125 (апрель 2024), в Safari — в 17.4 (март 2024). Конструктор на верхнем
// уровне модуля брошенным исключением уронил бы весь экран у читателя со
// старым браузером ещё до первого рендера, поэтому обёрнут в try/catch — без
// сегментатора diffWords просто вернётся к обычному словарю jsdiff
// (посимвольно на кириллице), а не к белому экрану.
const RU_WORD_SEGMENTER = (() => {
  try {
    return new Intl.Segmenter('ru', { granularity: 'word' });
  } catch {
    return undefined;
  }
})();

// Потолок расстояния правки. Пословный дифф у jsdiff — алгоритм Майерса,
// цена O(N·D): не от объёма полосы, а от объёма ПРАВКИ в ней. Замер на
// настоящем diff@7 с этим же сегментатором (frontend, node 24):
//
//   20 000 слов,     1 правка   —   18 мс  (типичный вклад читателя)
//   20 000 слов,   200 правок   —   35 мс
//   20 000 слов, 1 000 правок   —  426 мс
//   20 000 слов, 4 000 правок   — 5 088 мс, дальше React рисует 12 000 узлов
//
// а ветка «только пробелы» (diffWordsWithSpace, ниже) на 20 000 слов с
// удвоенными пробелами не досчиталась за две минуты вовсе — то есть намертво
// повешенная вкладка. Две самые длинные полосы корпуса — ровно около 20 000
// слов, и экран разбора устаревшего предложения рисует ДВА диффа.
//
// Ограничитель ставится по расстоянию, а не по объёму полосы, намеренно:
// порог по числу слов огрубил бы как раз тот случай, который работает
// отлично и нужен чаще всего — одна опечатка на огромной полосе. С
// maxEditLength = 1000 ни один замеренный случай не превышает ~100 мс: либо
// дифф считается (до 1 000 правок), либо jsdiff сдаётся и мы показываем
// построчный, который считается за доли миллисекунды.
const MAX_EDIT_LENGTH = 1000;

// Типы @types/diff обещают Change[] и про undefined не знают, хотя оно
// документировано у самого maxEditLength («jsdiff will return undefined
// instead of a diff») и проверено замером. Отсюда приведение на обоих
// вызовах: без него ветка «jsdiff сдался» выглядела бы недостижимой, и
// сборка бы её выбросила.
type BoundedDiff = Change[] | undefined;

type DiffMode = 'words' | 'whitespace' | 'lines';

/**
 * Пословный дифф. Построчного здесь мало: правка одной буквы в абзаце даёт
 * «строка удалена, строка добавлена», и редактор ищет букву глазами — ровно
 * та работа, ради устранения которой предложения подаются целой полосой.
 *
 * На правке, слишком крупной для пословного сравнения, дифф честно
 * огрубляется до построчного и говорит об этом вслух: замолчать подмену
 * нельзя, дифф — единственный инструмент редактора, и он обязан знать, что
 * видит грубую картину.
 */
export const MarkdownDiff: React.FC<MarkdownDiffProps> = ({ before, after }) => {
  const { parts, mode } = useMemo((): { parts: Change[]; mode: DiffMode } => {
    const wordParts = diffWords(before, after, {
      intlSegmenter: RU_WORD_SEGMENTER,
      maxEditLength: MAX_EDIT_LENGTH,
    }) as BoundedDiff;
    if (!wordParts) {
      return { parts: diffLines(before, after), mode: 'lines' };
    }

    // diffWords сравнивает токены по .trim() (jsdiff's wordDiff.equals) —
    // правка, состоящая только из пробелов или переносов строк (пустая
    // строка, вставленная в стих; пробел перед маркером сноски), даёт НОЛЬ
    // added/removed кусков, даже когда строки не равны. В этом случае
    // пересчитываем diffWordsWithSpace: слова совпадают, отличаются только
    // пробелы — сравнивать по буквам здесь нечего, посимвольного шума на
    // кириллице поэтому не возникает (проверено в MarkdownDiff.test.tsx).
    const hasVisibleDiff = wordParts.some((p) => p.added || p.removed);
    if (!hasVisibleDiff && before !== after) {
      const spaceParts = diffWordsWithSpace(before, after, {
        maxEditLength: MAX_EDIT_LENGTH,
      }) as BoundedDiff;
      if (!spaceParts) {
        return { parts: diffLines(before, after), mode: 'lines' };
      }
      return { parts: spaceParts, mode: 'whitespace' };
    }
    return { parts: wordParts, mode: 'words' };
  }, [before, after]);

  return (
    <div className="markdown-diff">
      {mode === 'whitespace' && (
        <div className="markdown-diff-note">
          Правка только в пробелах и переносах строк — слова не менялись
        </div>
      )}
      {mode === 'lines' && (
        <div className="markdown-diff-note">
          Правка слишком велика для пословного сравнения — показан построчный дифф: изменённые
          строки видны целиком, отдельные слова в них не выделены.
        </div>
      )}
      {parts.map((part, i) => {
        if (part.added) {
          return (
            <ins key={i} data-testid="diff-added" className="diff-added">
              {part.value}
            </ins>
          );
        }
        if (part.removed) {
          return (
            <del key={i} data-testid="diff-removed" className="diff-removed">
              {part.value}
            </del>
          );
        }
        return <span key={i}>{part.value}</span>;
      })}
    </div>
  );
};
