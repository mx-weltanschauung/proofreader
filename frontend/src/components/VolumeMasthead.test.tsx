import { describe, it, expect, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter, Edition, PageMapEntry, Work } from '../types';
import type { LastRead } from '../hooks/useReadingProgress';
import { VolumeMasthead } from './VolumeMasthead';
import { shareFrom, stubShare, unstubShare } from '../test/shareStub';

const WORK = {
  id: 41,
  title: 'К. Маркс и Ф. Энгельс. Сочинения. Том 16',
  edition_id: 1,
  volume_number: 16,
  page_offset: 0,
} as Work;

const EDITION = { id: 1, title: 'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.' } as Edition;

const CHAPTERS = [
  { id: 1664, work_id: 41, title: 'К. Маркс. Манифест', start_page: 3, end_page: 11 } as Chapter,
];

const PAGES: PageMapEntry[] = [
  { page_number: 1, status: 'не_вычитана' },
  { page_number: 2, status: 'вычитано_машиной' },
];

function setup(props: Partial<React.ComponentProps<typeof VolumeMasthead>> = {}) {
  return render(
    <MemoryRouter>
      <VolumeMasthead
        work={WORK}
        edition={EDITION}
        chapters={CHAPTERS}
        pages={PAGES}
        lastRead={null}
        {...props}
      />
    </MemoryRouter>,
  );
}

describe('VolumeMasthead', () => {
  it('называет том и ведёт в собрание', () => {
    setup();
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
      'К. Маркс и Ф. Энгельс. Сочинения. Том 16',
    );
    expect(
      screen.getByRole('link', { name: /К\. Маркс и Ф\. Энгельс\. Сочинения, 2-е изд\./ }),
    ).toHaveAttribute('href', '/editions/1');
  });

  it('считает работы и страницы', () => {
    // Точный текст целиком, не подстрока: хвост про вычитку убран, и
    // поиск по подстроке его возвращения не заметил бы.
    setup();
    expect(screen.getByText('1 работа · 2 страницы')).toBeInTheDocument();
  });

  it('не считает вычитанное — ни машиной, ни человеком', () => {
    setup({
      pages: [
        { page_number: 1, status: 'вычитана' },
        { page_number: 2, status: 'вычитано_машиной' },
      ],
    });
    expect(screen.getByText('1 работа · 2 страницы')).toBeInTheDocument();
    expect(screen.queryByText(/вычитано/)).toBeNull();
  });

  it('«Читать с начала» ведёт в первую главу', () => {
    setup();
    expect(screen.getByRole('link', { name: 'Читать с начала' })).toHaveAttribute(
      'href',
      '/works/41/chapters/1664',
    );
  });

  it('предлагает скачать том рядом с кнопками чтения', () => {
    setup();
    expect(screen.getByRole('button', { name: 'Скачать' })).toBeInTheDocument();
  });

  it('карта страниц пуста, но главы есть — кнопки чтения остаются, строки с числами нет', () => {
    setup({ pages: [] });
    expect(screen.getByRole('link', { name: 'Читать с начала' })).toHaveAttribute(
      'href',
      '/works/41/chapters/1664',
    );
    expect(screen.queryByText(/страни/)).toBeNull();
  });

  it('нет ни глав, ни карты страниц — кнопок чтения нет', () => {
    setup({ pages: [], chapters: [] });
    expect(screen.queryByRole('link', { name: 'Читать с начала' })).not.toBeInTheDocument();
  });

  it('без глав «Читать с начала» ведёт на первую страницу', () => {
    setup({ chapters: [] });
    expect(screen.getByRole('link', { name: 'Читать с начала' })).toHaveAttribute(
      'href',
      '/works/41/pages/1',
    );
  });

  it('показывает «Продолжить» только для этого тома', () => {
    const lastRead: LastRead = {
      workId: 41,
      chapterId: 1670,
      workTitle: 'Том 16',
      chapterTitle: 'О Прудоне',
      pageNumber: 27,
      ts: 0,
    };
    const { unmount } = setup({ lastRead });
    expect(screen.getByRole('link', { name: 'Продолжить: О Прудоне, стр. 27' })).toHaveAttribute(
      'href',
      '/works/41/chapters/1670',
    );
    unmount();

    setup({ lastRead: { ...lastRead, workId: 7 } });
    expect(screen.queryByRole('link', { name: /Продолжить/ })).not.toBeInTheDocument();
  });

  it('перечисляет предваряющие материалы', () => {
    setup({ work: { ...WORK, children: [{ id: 42, title: 'Титульный лист' } as Work] } as Work });
    expect(screen.getByRole('link', { name: 'Титульный лист' })).toHaveAttribute(
      'href',
      '/works/42',
    );
  });

  it('показывает описание тома, если оно есть', () => {
    setup({ work: { ...WORK, description: 'Первая часть собрания сочинений.' } as Work });
    expect(screen.getByText('Первая часть собрания сочинений.')).toBeInTheDocument();
  });

  it('не показывает описание, если оно пустое или отсутствует', () => {
    const { unmount } = setup({ work: { ...WORK, description: '   ' } as Work });
    expect(document.querySelector('.vol-masthead-description')).toBeNull();
    unmount();

    setup({ work: { ...WORK, description: undefined } as Work });
    expect(document.querySelector('.vol-masthead-description')).toBeNull();
  });

  it('в томе под одной обёрткой считает работы, а не обёртку', () => {
    setup({
      chapters: [
        {
          id: 1875,
          work_id: 41,
          title: 'Искусство и литература',
          start_page: 1,
          end_page: 2,
          children: [
            {
              id: 1876,
              work_id: 41,
              title: 'Письма без адреса',
              start_page: 1,
              end_page: 1,
            } as Chapter,
            { id: 1881, work_id: 41, title: 'Генрик Ибсен', start_page: 2, end_page: 2 } as Chapter,
          ],
        } as Chapter,
      ],
    });
    expect(screen.getByText('2 работы · 2 страницы')).toBeInTheDocument();
  });

  describe('Поделиться', () => {
    afterEach(unstubShare);

    it('отдаёт том с автором и каноническим адресом', async () => {
      const share = stubShare();
      setup({ work: { ...WORK, slug: 'mae-t16', author: 'К. Маркс, Ф. Энгельс' } as Work });
      const data = await shareFrom(share);
      expect(data.title).toBe('К. Маркс и Ф. Энгельс. Сочинения. Том 16 — К. Маркс, Ф. Энгельс');
      expect(data.url).toBe(`${window.location.origin}/works/41-mae-t16`);
    });

    it('без автора подписывает том одним названием', async () => {
      const share = stubShare();
      setup({ work: { ...WORK, author: '' } as Work });
      expect((await shareFrom(share)).title).toBe('К. Маркс и Ф. Энгельс. Сочинения. Том 16');
    });
  });

  it('номер журнала: eyebrow ведёт в журнал, «т.» и собрания нет', () => {
    setup({
      work: {
        id: 90,
        edition_id: 7,
        title: 'Под знаменем марксизма, 1925, № 5—6',
        page_offset: 0,
        journal_issue: {
          issue_id: 1,
          journal_id: 2,
          journal_slug: 'pzm',
          journal_title: 'Под знаменем марксизма',
          year: 1925,
          label: '5—6',
          months: '',
        },
      } as Work,
      edition: null,
    });
    expect(screen.getByRole('link', { name: 'Под знаменем марксизма' })).toHaveAttribute(
      'href',
      '/journals/pzm',
    );
    expect(screen.getByText(/Журналы/)).toHaveTextContent(
      'Журналы · Под знаменем марксизма · 1925',
    );
    expect(screen.queryByText(/т\./)).toBeNull();
    expect(document.querySelector('a[href^="/editions"]')).toBeNull();
  });
});
