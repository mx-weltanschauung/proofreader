import { render } from '@testing-library/react';
import { MemoryRouter, useNavigate } from 'react-router-dom';
import { useEffect } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { PageViewBeacon } from './PageViewBeacon';

const sendHit = vi.fn();
vi.mock('../services/hit', () => ({
  sendHit: (...args: unknown[]) => sendHit(...args),
  externalReferrer: () => 'https://yandex.ru/',
}));

function Go({ to }: { to: string[] }) {
  const navigate = useNavigate();
  useEffect(() => {
    to.forEach((p) => navigate(p));
  }, [navigate, to]);
  return null;
}

describe('PageViewBeacon', () => {
  beforeEach(() => sendHit.mockClear());

  it('шлёт маячок на каждую смену пути, источник — только первым', () => {
    render(
      <MemoryRouter initialEntries={['/works/1']}>
        <PageViewBeacon />
        <Go to={['/works/1/chapters/2', '/works/1/chapters/2?from=5']} />
      </MemoryRouter>,
    );
    expect(sendHit.mock.calls).toEqual([
      ['/works/1', 'https://yandex.ru/'],
      ['/works/1/chapters/2', ''],
    ]);
  });
});
