import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ReadingSurface } from './ReadingSurface';
import { HintsProvider } from '../contexts/HintsProvider';
import { HINTS, HINT_APPEAR_MS } from '../hints/registry';
import { installHintHarness, removeHintHarness, sayVisible, advance } from '../test/hintHarness';
import type { ChapterPage } from '../types';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';

/**
 * Экран главы и чтения подборки. Здесь своя кнопка номеров страниц —
 * текстовая «Номера страниц», не значок «№» из панели потокового чтения, — и
 * до этой проверки она была единственной возможностью читальни, у которой
 * подсказки не было вовсе: план привязал выноску только к потоковой копии.
 */
const PAGE: ChapterPage = { page_number: 1, html: '<p>текст</p>', blank: false };

beforeEach(() => {
  localStorage.clear();
  installHintHarness();
});

afterEach(removeHintHarness);

describe('выноски экрана главы', () => {
  it('объясняет кнопку номеров страниц', () => {
    render(
      <MemoryRouter>
        <ReadingPreferencesProvider>
          <HintsProvider>
            <ReadingSurface
              pages={[PAGE]}
              footnotesHtml=""
              work={{ id: 46, page_offset: 0 }}
              header={<h1>Глава</h1>}
              pageHref={() => '/works/46/pages/1'}
            />
          </HintsProvider>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    sayVisible(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toHaveTextContent(HINTS['page-numbers'].text);
  });
});
