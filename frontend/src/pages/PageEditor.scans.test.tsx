import { afterEach, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { chaptersApi, pagesApi, worksApi } from '../services/api';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { PageEditor } from './PageEditor';

const ok = <T,>(data: T) => Promise.resolve({ data } as AxiosResponse<T>);
afterEach(() => vi.restoreAllMocks());

it('редактор видит текст скрытой полосы без изображения', async () => {
  vi.spyOn(pagesApi, 'getByNumber').mockReturnValue(
    ok({
      id: 4,
      work_id: 1,
      page_number: 5,
      content_markdown: 'Текст доступен',
      status: 'вычитана',
      preview_path: 'works/1/pages/leak.png',
      preview_url: 'https://example.test/leak.png',
      scan_restricted: true,
    }) as never,
  );
  vi.spyOn(worksApi, 'get').mockReturnValue(ok({ id: 1, title: 'Том', page_offset: 0 }) as never);
  vi.spyOn(chaptersApi, 'list').mockReturnValue(ok([]) as never);
  render(
    <ReadingPreferencesProvider>
      <MemoryRouter initialEntries={['/works/1/pages/5/edit']}>
        <Routes>
          <Route path="/works/:workId/pages/:pageNumber/edit" element={<PageEditor />} />
        </Routes>
      </MemoryRouter>
    </ReadingPreferencesProvider>,
  );
  expect(await screen.findByDisplayValue('Текст доступен')).toBeInTheDocument();
  expect(screen.getByText('Скан доступен только администратору')).toBeInTheDocument();
  expect(document.querySelector('img[src*="leak.png"]')).toBeNull();
});
