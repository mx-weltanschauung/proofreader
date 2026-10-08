import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ReadingStreamBar } from './ReadingStreamBar';
import { HintsProvider } from '../contexts/HintsProvider';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { HINTS, HINT_APPEAR_MS } from '../hints/registry';
import {
  installHintHarness,
  removeHintHarness,
  sayVisible as say,
  advance,
} from '../test/hintHarness';

beforeEach(() => {
  localStorage.clear();
  installHintHarness();
});

afterEach(removeHintHarness);

describe('выноски панели чтения', () => {
  it('объясняет кнопку номеров страниц', () => {
    render(
      <MemoryRouter>
        {/* Панель монтирует ReadingSettings, а тому нужен свой провайдер. */}
        <ReadingPreferencesProvider>
          <HintsProvider>
            <ReadingStreamBar
              chapterTitle="Глава II"
              progressPercent={10}
              backHref="/works/46"
              showPageNumbers={false}
              onTogglePageNumbers={() => {}}
              suggestHref={null}
              scanHref={null}
              contentRef={{ current: null }}
              visiblePage={null}
            />
          </HintsProvider>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    say(true);
    advance(HINT_APPEAR_MS + 100);
    // Из двух выносок панели «№» приоритетнее «Aa» (100 против 90).
    expect(screen.getByRole('status')).toHaveTextContent(HINTS['page-numbers'].text);
  });
});
