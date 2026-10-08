/**
 * Представительный набор формул для замера вывода KaTeX.
 *
 * Из него выведен белый список инлайновых стилей в
 * `src/components/rehypeAllowlistStyles.ts` (скрипт
 * `scripts/measure-katex-styles.mjs`), и на нём же стоит сторожевой тест
 * `rehypeAllowlistStyles.test.ts`: ни одна декларация `style`, которую
 * KaTeX выдаёт на этих формулах, не должна теряться на фильтре.
 *
 * Набор покрывает то, что реально встречается в корпусе читальни и что
 * перечислено в задаче: дроби, радикалы (включая \sqrt[3]), матрицы и
 * array, суммы и интегралы с пределами, \underbrace/\overbrace, \text,
 * составные акценты, разделители \left…\right, рамки, цвет, фантомы.
 */
export const KATEX_SAMPLE_FORMULAS = [
  // дроби
  '$\\frac{a}{b}$',
  '$$\\frac{\\partial f}{\\partial x}$$',
  '$\\dfrac{1}{1+\\frac{1}{x}}$',
  '$\\tfrac{3}{4}$',
  '$\\binom{n}{k}$',
  '$\\cfrac{1}{2+\\cfrac{3}{4}}$',
  // радикалы
  '$\\sqrt{2}$',
  '$\\sqrt[3]{x+y}$',
  '$\\sqrt[n]{\\frac{a}{b}}$',
  '$\\sqrt{\\sqrt{\\sqrt{x}}}$',
  // матрицы и таблицы
  '$$\\begin{matrix} a & b \\\\ c & d \\end{matrix}$$',
  '$$\\begin{pmatrix} 1 & 2 \\\\ 3 & 4 \\end{pmatrix}$$',
  '$$\\begin{bmatrix} x \\\\ y \\end{bmatrix}$$',
  '$$\\begin{vmatrix} a & b \\\\ c & d \\end{vmatrix}$$',
  '$$\\begin{cases} x, & x>0 \\\\ -x, & x\\le 0 \\end{cases}$$',
  '$$\\begin{array}{c|c} a & b \\\\ \\hline c & d \\end{array}$$',
  '$$\\begin{aligned} a &= b \\\\ c &= d \\end{aligned}$$',
  '$\\begin{smallmatrix} 1&2 \\\\ 3&4 \\end{smallmatrix}$',
  '$\\begin{gather} a=b \\\\ c=d \\end{gather}$',
  // суммы и интегралы с пределами
  '$$\\sum_{i=1}^{n} i^2$$',
  '$\\sum_{i=1}^{n} i$',
  '$$\\int_0^1 x^2\\,dx$$',
  '$$\\iint_D f\\,dA$$',
  '$$\\oint_C \\vec F\\cdot d\\vec r$$',
  '$$\\prod_{k=1}^{\\infty} a_k$$',
  '$$\\lim_{x \\to 0^+} \\frac{\\sin x}{x}$$',
  '$$\\bigcup_{i\\in I} A_i$$',
  '$$\\max_{x\\in X} f(x)$$',
  '$\\displaystyle\\sum_{n=0}^\\infty \\frac{1}{n!}$',
  // подчёркивающие и надчёркивающие скобки
  '$$\\underbrace{a+b+c}_{\\text{три слагаемых}}$$',
  '$$\\overbrace{x_1+\\cdots+x_n}^{n\\ \\text{штук}}$$',
  '$$\\underset{n\\to\\infty}{\\lim} a_n$$',
  '$$\\overset{def}{=}$$',
  '$$\\underline{ab}\\ \\overline{AB}$$',
  '$\\overgroup{ab}\\ \\undergroup{cd}$',
  '$\\utilde{x}\\ \\uline{y}$',
  // текст и начертания
  '$\\text{прибавочная стоимость}$',
  '$\\textbf{жирный}\\ \\textit{курсив}$',
  '$\\mathrm{d}x$',
  '$\\mathbb{R}^n$',
  '$\\mathcal{L}$',
  '$\\mathfrak{g}$',
  '$\\pmb{bold}$',
  '$\\scriptstyle small \\scriptscriptstyle tiny$',
  // акценты, в том числе составные
  '$\\hat{x}\\ \\bar{y}\\ \\vec{v}\\ \\dot{z}\\ \\ddot{z}\\ \\tilde{u}\\ \\breve{a}\\ \\check{c}$',
  '$\\widehat{ABC}\\ \\widetilde{xyz}$',
  '$\\hat{\\hat{x}}$',
  '$\\overrightarrow{AB}\\ \\overleftarrow{CD}\\ \\overleftrightarrow{EF}$',
  '$\\overset{\\frown}{AB}$',
  // разделители
  '$\\left(\\frac{a}{b}\\right)$',
  '$\\left[\\sum_i x_i\\right]$',
  '$\\left\\{ \\frac{p}{q} \\right\\}$',
  '$\\big| x \\big|$',
  // стрелки, надстрочные метки
  '$x \\xrightarrow{f} y$',
  '$a \\stackrel{?}{=} b$',
  '$\\substack{a \\\\ b}$',
  '$\\tag{1} x=y$',
  // рамки, вычёркивания, фантомы, отбивки
  '$\\boxed{x+y}$',
  '$\\not=\\ \\cancel{x}$',
  '$\\phantom{x}\\ \\hphantom{y}\\ \\vphantom{z}$',
  '$\\mathstrut a$',
  '$\\smash{y}$',
  '$\\rule{2em}{1pt}$',
  '$\\raisebox{2pt}{x}$',
  '$\\hspace{1em}a\\ \\kern2pt b\\ \\mkern3mu c$',
  '$\\genfrac{}{}{0pt}{}{a}{b}$',
  // цвет
  '$\\color{red}{x}$',
  '$\\textcolor{blue}{y}$',
  // прочее из корпуса
  '$P(A\\mid B)=\\frac{P(B\\mid A)P(A)}{P(B)}$',
  '$x_1^2 + x_{i,j}^{n+1}$',
  '$e^{-\\frac{x^2}{2}}$',
  '$\\%\\ 100\\%\\ \\$5$',
  "$f'(x)\\ f''(x)$",
  '$\\alpha\\beta\\gamma\\Delta\\Omega\\pi\\sigma$',
  '$\\vcentcolon$',
  '$\\htmlClass{foo}{x}$',
];
