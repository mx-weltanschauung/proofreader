import { describe, expect, it } from 'vitest';
import { documentPath, documentEditPath } from './documentPaths';

describe('адрес разбора', () => {
  it('у сотруднического короткий', () => {
    expect(documentPath({ slug: 'o-gosudarstve', author_nickname: '' })).toBe(
      '/documents/o-gosudarstve',
    );
  });

  it('у читательского несёт подпись', () => {
    expect(documentPath({ slug: 'chto-delat', author_nickname: 'chitatel' })).toBe(
      '/documents/chitatel/chto-delat',
    );
  });

  it('правка идёт тем же адресом с хвостом', () => {
    expect(documentEditPath({ slug: 'chto-delat', author_nickname: 'chitatel' })).toBe(
      '/documents/chitatel/chto-delat/edit',
    );
  });
});
