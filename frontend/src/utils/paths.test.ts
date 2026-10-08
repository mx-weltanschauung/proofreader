import { describe, it, expect } from 'vitest';
import { numericId, workPath, chapterPath, pagePath, readPath, editionPath } from './paths';

const work = { id: 49, slug: 'lenin-t06' };
const chapter = { id: 10125, slug: 'chto-delat' };

describe('paths', () => {
  it('номер берётся из ведущих цифр сегмента', () => {
    expect(numericId('49-lenin-t06')).toBe(49);
    expect(numericId('49')).toBe(49);
    // Не parseInt: у Number() здесь NaN, а шаблон адреса API подставил бы
    // сегмент целиком и молча уехал в 400.
    expect(numericId('abc')).toBeNull();
    expect(numericId(undefined)).toBeNull();
  });

  it('строит адреса со слагом', () => {
    expect(workPath(work)).toBe('/works/49-lenin-t06');
    expect(chapterPath(work, chapter)).toBe('/works/49-lenin-t06/chapters/10125-chto-delat');
    expect(pagePath(work, 233)).toBe('/works/49-lenin-t06/pages/233');
    expect(readPath(work, 233)).toBe('/works/49-lenin-t06/read/233');
    expect(editionPath({ id: 4, url_slug: 'lenin' })).toBe('/editions/4-lenin');
  });

  it('без слага печатает голый номер', () => {
    // Ответ API мог не нести слаг (покрытие доводится постепенно). Такой
    // адрес рабочий: краулеру ветка /seo отдаёт 301 на канон, читателю
    // строка адреса тихо подменяется.
    expect(workPath({ id: 49 })).toBe('/works/49');
    expect(chapterPath({ id: 49 }, { id: 10240, slug: '' })).toBe('/works/49/chapters/10240');
    expect(editionPath({ id: 4 })).toBe('/editions/4');
  });

  it('пустой url_slug издания откатывается на slug — как на сервере', () => {
    // effectiveEditionSlug (internal/seo/canonical.go) делает тот же откат:
    // без него канон, который печатает краулеру /seo, и адрес в строке
    // браузера у читателя расходятся.
    expect(editionPath({ id: 4, slug: 'lenin-pss-5', url_slug: '' })).toBe(
      '/editions/4-lenin-pss-5',
    );
    // url_slug заполнен — он и остаётся ведущим, slug не подмешивается.
    expect(editionPath({ id: 4, slug: 'lenin-pss-5', url_slug: 'lenin' })).toBe(
      '/editions/4-lenin',
    );
  });
});
