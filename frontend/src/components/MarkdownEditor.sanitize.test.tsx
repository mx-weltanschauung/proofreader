import { describe, it, expect } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { MarkdownEditor } from './MarkdownEditor';

// Круг правок 2: хранимая XSS через предпросмотр markdown. В отличие от
// MarkdownEditor.test.tsx, здесь `@uiw/react-md-editor` НЕ замокан — нужен
// настоящий rehype-конвейер (rehype-raw -> rehype-katex -> rehype-sanitize),
// а не заглушка, игнорирующая previewOptions. PageSuggest.test.tsx уже так
// рендерит реальный MDEditor в jsdom без мока.

const XSS_PAYLOAD =
  'Текст читателя <img src=x onerror="window.__pwned = true"> и <script>window.__pwned = true</script> ещё текст';

const FORMULA_TEXT = 'Формула инлайн $x^2 + \\sqrt{y}$ в предложении читателя';

// Круг правок 3: schema allow-лист span'а на style верен (иначе KaTeX не
// сверстать), но не проверяет *значение* — url()/@import в style читателя
// доезжают до DOM браузера модератора и утекают IP + Referer на хост
// нападающего, кода при этом не исполняется.
const STYLE_URL_PAYLOAD =
  'Текст читателя <span style="background:url(https://attacker.example/x.png)">маячок</span> ещё текст';
const STYLE_IMPORT_PAYLOAD =
  'Текст читателя <span style="@import url(https://attacker.example/z.css);">маячок</span> ещё текст';

// Круг правок 4: чёрный список круга 3 (регулярка на url()/@import)
// обходится экранами CSS — по CSS Syntax L3 они раскрываются до сравнения
// с «url», поэтому браузер видит настоящий url() там, где регулярка его не
// видит. Логика перевёрнута на белый список формы значения.
//
// Сами escape-нагрузки проверяются НЕ здесь, а в
// rehypeAllowlistStyles.test.ts — по строке HTML, которую отдаёт rehype.
// Причина замерена, а не предположена: jsdom — не браузер, его CSS-парсер
// сам отвергает `background:\75rl(...)`, `image-set(...)` и `@import`, и
// на несанитизированном пути они до innerHTML не доезжают вовсе. Тест на
// них здесь был бы зелёным и вовсе без фильтра, то есть не доказывал бы
// ничего. jsdom сохраняет обычный `url()`, `background:red` и длины — вот
// на них здесь и стоят проверки проводки.
const STYLE_MIXED_PAYLOAD =
  'Текст читателя <span style="height:1em;background:red">маячок</span> ещё текст';

function renderEditor(content: string, sanitizeUntrustedContent: boolean) {
  return render(
    <ReadingPreferencesProvider>
      <MarkdownEditor
        initialContent={content}
        onChange={() => {}}
        sanitizeUntrustedContent={sanitizeUntrustedContent}
      />
    </ReadingPreferencesProvider>,
  );
}

describe('MarkdownEditor — безопасный режим предпросмотра', () => {
  it('в safe-режиме payload из <img onerror> / <script> не попадает в DOM живым узлом', async () => {
    const { container } = renderEditor(XSS_PAYLOAD, true);

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    // <script> целиком вырезается (тег и содержимое) — сработавший payload
    // объявил бы window.__pwned.
    expect(container.querySelector('script')).toBeNull();
    expect((window as unknown as { __pwned?: boolean }).__pwned).toBeUndefined();

    // <img> может остаться (src сам по себе не опасен), но onerror снят —
    // атрибута обработчика в DOM нет вовсе.
    const img = preview.querySelector('img');
    if (img) {
      expect(img.getAttribute('onerror')).toBeNull();
      expect(img.outerHTML).not.toMatch(/onerror/i);
    }
    expect(preview.innerHTML).not.toMatch(/onerror/i);
    expect(preview.textContent).toContain('Текст читателя');
    expect(preview.textContent).toContain('ещё текст');
  });

  it('без safe-режима (доверенный редактор) поведение осталось прежним — <script> всё ещё живой узел в дереве', async () => {
    const { container } = renderEditor(XSS_PAYLOAD, false);

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    // Без явного opt-in дефолтное поведение MarkdownEditor трогать нельзя
    // (пропущено ровно так специально — доверенный экран вычитчика не должен
    // менять поведение из-за этой задачи). rehype-raw по-прежнему без
    // санитайзера превращает <script> в настоящий узел дерева — это и есть
    // дыра, которую safe-режим закрывает только там, где его явно запросили.
    expect(preview.querySelector('script')).not.toBeNull();
    expect(preview.querySelector('script')?.textContent).toContain('__pwned');
  });

  it('в safe-режиме формула по-прежнему рисуется (KaTeX не срезан вместе с payload)', async () => {
    const { container } = renderEditor(FORMULA_TEXT, true);

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    await waitFor(() => {
      expect(preview.querySelector('.katex')).toBeInTheDocument();
    });

    const katexNode = preview.querySelector('.katex') as HTMLElement;
    // MathML-аннотация несёт исходный TeX — то, что она на месте, значит
    // rehype-sanitize не срезал math/annotation вместе с посторонним HTML.
    expect(katexNode.querySelector('annotation')?.textContent).toContain('x^2');
    // katex-html — параллельная HTML-разметка формулы; её присутствие
    // означает, что схема санитайзера пропустила span[class]/svg/path.
    expect(katexNode.querySelector('.katex-html')).toBeInTheDocument();
  });

  it('в safe-режиме style="background:url(...)" на span не доезжает до DOM со своим значением', async () => {
    const { container } = renderEditor(STYLE_URL_PAYLOAD, true);

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    await waitFor(() => {
      expect(preview.textContent).toContain('маячок');
    });

    // Атрибут style мог остаться (например, пустым) или пропасть целиком —
    // важно только то, что url(...) в его значении нет нигде в дереве.
    expect(preview.innerHTML).not.toMatch(/url\s*\(/i);
    expect(preview.innerHTML).not.toContain('attacker.example');
  });

  it('в safe-режиме @import в style тоже вычищается — тот же класс риска, что url()', async () => {
    const { container } = renderEditor(STYLE_IMPORT_PAYLOAD, true);

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    await waitFor(() => {
      expect(preview.textContent).toContain('маячок');
    });

    expect(preview.innerHTML).not.toMatch(/@import/i);
    expect(preview.innerHTML).not.toContain('attacker.example');
  });

  it('в safe-режиме из style остаётся только вёрстка по белому списку, а не всё, что не url()', async () => {
    // Разграничительный тест старого и нового поведения. Чёрный список
    // круга 3 оставил бы тут `background:red` нетронутым — url() в нём нет.
    // Белый список оставляет только длину, которую понимает вёрстка KaTeX.
    const { container } = renderEditor(STYLE_MIXED_PAYLOAD, true);

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    await waitFor(() => {
      expect(preview.textContent).toContain('маячок');
    });

    const span = [...preview.querySelectorAll('span')].find((node) =>
      node.textContent?.includes('маячок'),
    );
    expect(span?.getAttribute('style')).toMatch(/height:\s*1em/);
    expect(span?.getAttribute('style')).not.toMatch(/background/);
  });

  it.each([
    [
      'markdown-картинка',
      'Текст читателя\n\n![подпись](https://attacker.example/x.png)\n\nещё текст',
    ],
    ['сырой img', 'Текст читателя <img src="https://attacker.example/y.png"> ещё текст'],
  ])(
    'в safe-режиме картинка не доезжает до DOM узлом, способным сходить в сеть: %s',
    async (_name, payload) => {
      // Круг правок 5. Утечка IP модератора была не только в style: markdown
      // `![](https://чужой/x.png)` давал живой <img>, и браузер шёл на чужой
      // хост сам, при открытии карточки. Замер по рабочей базе: 0 картинок
      // на 45 495 полос корпуса — законных случаев нет, запрет бесплатен.
      const { container } = renderEditor(payload, true);

      await waitFor(() => {
        expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
      });
      const preview = container.querySelector('.wmde-markdown') as HTMLElement;

      await waitFor(() => {
        expect(preview.textContent).toContain('Текст читателя');
      });

      expect(preview.querySelector('img')).toBeNull();
      expect(preview.innerHTML).not.toContain('attacker.example');
    },
  );

  it('в safe-режиме краска SVG с url() не доезжает до DOM — узел жив, атрибут снят', async () => {
    // Живая протечка, пережившая круги 4 и 5: SVG-краска принимает url(),
    // и `<line stroke="url(https://чужой/x.svg#g)">` из обычного markdown
    // читателя тянул ресурс с чужого хоста при отрисовке — ни клика, ни
    // CSS-трюка.
    //
    // Тест не пустой, и это проверено замером до того, как ему поверить:
    // на несанитизированном пути jsdom доносит атрибут до DOM целиком
    // (`stroke=url(https://attacker.example/x.svg#g)`), в отличие от
    // CSS-экранов, которые его парсер отвергает сам. Значит пустой
    // `getAttribute('stroke')` в safe-режиме — настоящий сигнал.
    const { container } = renderEditor(
      'Текст читателя <svg width="20" height="20"><line x1="0" y1="0" x2="20" y2="20" stroke="url(https://attacker.example/x.svg#g)"/></svg> ещё текст',
      true,
    );

    await waitFor(() => {
      expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    await waitFor(() => {
      expect(preview.textContent).toContain('Текст читателя');
    });

    const line = preview.querySelector('line');
    expect(line).not.toBeNull();
    expect(line?.getAttribute('stroke')).toBeNull();
    expect(preview.innerHTML).not.toContain('attacker.example');
  });

  it('в safe-режиме формула цела после запрета сетевых узлов — \\cancel и \\vec на месте', async () => {
    // Парное требование к запрету: вычитание убрало из схемы svg use/image
    // и href, поэтому надо видеть, что KaTeX ими не пользуется.
    const { container } = renderEditor('$\\cancel{x}$ и $\\vec{v}$ и $\\sqrt{y}$', true);

    await waitFor(() => {
      expect(container.querySelector('.katex')).toBeInTheDocument();
    });
    const preview = container.querySelector('.wmde-markdown') as HTMLElement;

    await waitFor(() => {
      expect(preview.querySelector('line')).toBeInTheDocument();
    });
    expect(preview.querySelector('svg')).toBeInTheDocument();
    expect(preview.querySelector('path')).toBeInTheDocument();
  });

  it('в safe-режиме формула сохраняет свои инлайновые style — фильтр url() не задевает верстку KaTeX', async () => {
    const { container } = renderEditor(FORMULA_TEXT, true);

    await waitFor(() => {
      expect(container.querySelector('.katex')).toBeInTheDocument();
    });
    const katexNode = container.querySelector('.katex') as HTMLElement;

    // .strut — первый узел katex-html, у него всегда инлайновый style с
    // height/vertical-align. Если бы фильтр url() задел style без url(),
    // этот style пропал бы вместе с версткой формулы.
    const strut = katexNode.querySelector('.strut');
    expect(strut).not.toBeNull();
    expect(strut?.getAttribute('style')).toMatch(/height/);

    // Радикал sqrt рисуется SVG-обводкой — она тоже должна пережить фильтр,
    // не спутавшись с настоящим url() в его собственных path/svg атрибутах
    // (viewBox — не style, но лежит в том же поддереве).
    expect(katexNode.querySelector('svg')).toBeInTheDocument();
    expect(katexNode.querySelector('path')).toBeInTheDocument();
  });
});
