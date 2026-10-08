// Логика поиска по заглавиям статической читальни.
// Запуск: node --test internal/staticsite/assets/search.test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const S = require('./search.js');

const t = (kind, title) => [kind, title, '', kind + '.html'];

test('ё и регистр не мешают', () => {
  const r = S.match([t('chapter', 'Ёлка'), t('chapter', 'ГОСУДАРСТВО')], 'елк', 10);
  assert.equal(r.total, 1);
  assert.equal(S.match([t('chapter', 'ГОСУДАРСТВО')], 'госуд', 10).total, 1);
});

test('слово запроса совпадает с началом слова, а не с серединой', () => {
  assert.equal(S.match([t('chapter', 'Революция')], 'волюц', 10).total, 0);
});

test('совпасть должны все слова запроса', () => {
  const e = [t('chapter', 'Государство и революция')];
  assert.equal(S.match(e, 'гос рев', 10).total, 1);
  assert.equal(S.match(e, 'гос мир', 10).total, 0);
});

test('запрос короче двух знаков не ищет', () => {
  assert.ok(S.tooShort('г'));
  assert.ok(!S.tooShort('го'));
  assert.equal(S.match([t('chapter', 'Государство')], 'г', 10).total, 0);
});

test('тома и понятия раньше глав, внутри вида — порядок корпуса', () => {
  const e = [t('chapter', 'Метод 1'), t('work', 'Метод 2'), t('concept', 'Метод 3'), t('chapter', 'Метод 4')];
  const r = S.match(e, 'метод', 10);
  assert.deepEqual(r.items.map((x) => x[1]), ['Метод 2', 'Метод 3', 'Метод 1', 'Метод 4']);
});

test('total считает всё, items ограничены', () => {
  const e = Array.from({ length: 5 }, (_, i) => t('chapter', 'Глава ' + i));
  const r = S.match(e, 'глава', 2);
  assert.equal(r.total, 5);
  assert.equal(r.items.length, 2);
});

test('результат по тексту ведёт на подглаву с наибольшим числом совпадений и несёт том', () => {
  const d = {
    url: '/works/1/2.html',
    excerpt: 'страница',
    meta: { title: 'О кооперации', volume: 'Том 45' },
    sub_results: [
      { title: 'О кооперации', url: '/works/1/2.html', excerpt: 'верх', locations: [1] },
      { title: 'II', url: '/works/1/2.html#ch-7', excerpt: 'вторая', locations: [5, 9, 12] },
    ],
  };
  assert.deepEqual(S.resultTarget(d), {
    url: '/works/1/2.html#ch-7', title: 'О кооперации — II', volume: 'Том 45', excerpt: 'вторая',
  });
});

test('подзаголовок, повторяющий заглавие главы, к подписи не добавляется', () => {
  const d = {
    url: '/w/1.html', excerpt: 'x', meta: { title: 'О КООПЕРАЦИИ' },
    sub_results: [{ title: 'О КООПЕРАЦИИ33', url: '/w/1.html#o-kooperacii', excerpt: 'y', locations: [1] }],
  };
  assert.equal(S.resultTarget(d).title, 'О КООПЕРАЦИИ');
  assert.equal(S.resultTarget(d).url, '/w/1.html#o-kooperacii');
});

test('без подрезультатов — страница целиком', () => {
  const d = { url: '/works/1/2.html', excerpt: 'страница', meta: { title: 'Глава' } };
  assert.deepEqual(S.resultTarget(d), { url: '/works/1/2.html', title: 'Глава', volume: '', excerpt: 'страница' });
});
