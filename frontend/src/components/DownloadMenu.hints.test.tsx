import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { DownloadMenu } from './DownloadMenu';
import { HintsProvider } from '../contexts/HintsProvider';
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

describe('выноска скачивания', () => {
  it('объясняет кнопку «Скачать»', () => {
    render(
      <MemoryRouter>
        <HintsProvider>
          <DownloadMenu href="/api/works/46/download" />
        </HintsProvider>
      </MemoryRouter>,
    );

    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toHaveTextContent(HINTS.download.text);
  });
});
