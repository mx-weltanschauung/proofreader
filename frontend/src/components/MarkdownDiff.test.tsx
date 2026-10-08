import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MarkdownDiff } from './MarkdownDiff';

describe('MarkdownDiff', () => {
  it('выделяет изменённое слово, а не строку целиком', () => {
    render(
      <MarkdownDiff
        before="Пролетарiат всех стран, соединяйтесь"
        after="Пролетариат всех стран, соединяйтесь"
      />,
    );
    // Неизменённый хвост обязан остаться неизменённым: если подсветилась вся
    // строка, редактор снова ищет букву глазами.
    const removed = screen.getByTestId('diff-removed');
    expect(removed.textContent).toContain('Пролетарiат');
    expect(removed.textContent).not.toContain('соединяйтесь');
  });

  it('на одинаковых текстах не показывает изменений', () => {
    render(<MarkdownDiff before="Один и тот же текст" after="Один и тот же текст" />);
    expect(screen.queryByTestId('diff-removed')).not.toBeInTheDocument();
    expect(screen.queryByTestId('diff-added')).not.toBeInTheDocument();
  });

  it('правку из одних пробелов показывает, а не молчит пустым диффом', () => {
    // diffWords сравнивает токены по .trim() — пробел перед маркером сноски
    // не меняет ни одно слово, и без отдельного пути редактор увидел бы
    // пустой дифф на непустой правке (пробел перед сноской в разметке стиха
    // — не опечатка, а разница, которую нужно видеть).
    render(<MarkdownDiff before="текст[^1]" after="текст [^1]" />);
    const added = screen.getByTestId('diff-added');
    expect(added.textContent).toBe(' ');
    expect(screen.queryByTestId('diff-removed')).not.toBeInTheDocument();
    expect(screen.getByText(/правка только в пробелах/i)).toBeInTheDocument();
  });

  it('словесная правка остаётся пословной — подпись про пробелы не показывается', () => {
    // Сторожевой тест на regresию: если бы диф безусловно переключился на
    // diffWordsWithSpace (без intlSegmenter), кириллица снова диффилась бы
    // посимвольно — «Пролетарiат» разбился бы на «Пролетар» + «i»/«и» + «ат…»
    // вместо одного removed-узла с целым словом.
    render(
      <MarkdownDiff
        before="Пролетарiат всех стран, соединяйтесь"
        after="Пролетариат всех стран, соединяйтесь"
      />,
    );
    expect(screen.queryByText(/правка только в пробелах/i)).not.toBeInTheDocument();
    expect(screen.getByTestId('diff-removed').textContent).toBe('Пролетарiат');
    expect(screen.getByTestId('diff-added').textContent).toBe('Пролетариат');
  });

  it('огромную правку огрубляет до построчной и говорит об этом', () => {
    // Пословный дифф у jsdiff — Майерс, цена O(N·D) по объёму ПРАВКИ. Замер
    // на настоящем diff@7: 20 000 слов с 4 000 рассеянных правок — 5 секунд
    // на один дифф, а экран разбора устаревшего предложения рисует два.
    // Ветка «только пробелы» на таком объёме не досчиталась и за две минуты.
    // Правка ниже заведомо выше потолка расстояния (каждое второе слово).
    const words = Array.from({ length: 4000 }, (_, i) => `слово${i % 97}`);
    const before = words.join(' ');
    const after = words.map((w, i) => (i % 2 === 0 ? `${w}ы` : w)).join(' ');

    const started = performance.now();
    render(<MarkdownDiff before={before} after={after} />);
    const elapsed = performance.now() - started;

    // Редактор обязан УЗНАТЬ, что видит грубую картину: молчаливая подмена
    // диффа хуже медленного диффа.
    expect(screen.getByText(/показан построчный дифф/i)).toBeInTheDocument();
    // Изменения при этом показаны, а не проглочены.
    expect(screen.getAllByTestId('diff-added').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('diff-removed').length).toBeGreaterThan(0);
    // Порог щедрый: смысл не в микробенчмарке, а в том, что вкладка не висит.
    expect(elapsed).toBeLessThan(3000);
  });

  it('одна опечатка на огромной полосе остаётся пословной', () => {
    // Парное требование к ограничителю. Он поставлен по расстоянию правки, а
    // не по объёму полосы, именно ради этого случая: самый частый вклад
    // читателя — одна буква на длинной полосе, и огрублять его нельзя.
    const words = Array.from({ length: 4000 }, (_, i) => `слово${i % 97}`);
    const before = words.join(' ');
    const after = before.replace('слово0 ', 'слов0 ');

    render(<MarkdownDiff before={before} after={after} />);

    expect(screen.queryByText(/показан построчный дифф/i)).not.toBeInTheDocument();
    expect(screen.getByTestId('diff-removed').textContent).toBe('слово0');
    expect(screen.getByTestId('diff-added').textContent).toBe('слов0');
  });
});
