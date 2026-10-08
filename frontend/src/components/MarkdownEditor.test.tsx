import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { MarkdownEditor } from './MarkdownEditor';

// Фабрика vi.mock поднимается выше импортов, поэтому JSX внутри неё
// использовать нельзя — собираем заглушку через createElement.
vi.mock('@uiw/react-md-editor', async () => {
  const { createElement } = await import('react');
  return {
    default: ({ value }: { value?: string }) =>
      createElement('div', { 'data-testid': 'md-editor' }, value),
  };
});

const SCAN_URL = 'https://example.test/scan-493.png';

function renderEditor(imageUrl?: string) {
  return render(
    <ReadingPreferencesProvider>
      <MarkdownEditor initialContent="текст страницы" onChange={() => {}} imageUrl={imageUrl} />
    </ReadingPreferencesProvider>,
  );
}

describe('MarkdownEditor', () => {
  it('со сканом открывается в режиме Редактор + скан', () => {
    const { container } = renderEditor(SCAN_URL);

    expect(container.querySelector('img')).toHaveAttribute('src', SCAN_URL);
    expect(screen.getByRole('button', { name: 'Редактор + скан' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Редактор + предпросмотр' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Редактор + скан' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'Редактор + предпросмотр' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    expect(screen.queryByRole('heading')).toBeNull();
  });

  it('без скана не показывает ни панель скана, ни переключатель', () => {
    const { container } = renderEditor(undefined);

    expect(container.querySelector('img')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Редактор + скан' })).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Редактор + предпросмотр' }),
    ).not.toBeInTheDocument();
  });

  it('переключение на Редактор + предпросмотр убирает панель скана, а обратно на Редактор + скан возвращает её', async () => {
    const user = userEvent.setup();
    const { container } = renderEditor(SCAN_URL);

    await user.click(screen.getByRole('button', { name: 'Редактор + предпросмотр' }));

    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByRole('button', { name: 'Редактор + предпросмотр' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'Редактор + скан' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    expect(screen.queryByRole('heading')).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Редактор + скан' }));

    expect(container.querySelector('img')).toHaveAttribute('src', SCAN_URL);
    expect(screen.getByRole('button', { name: 'Редактор + скан' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'Редактор + предпросмотр' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    expect(screen.queryByRole('heading')).toBeNull();
  });
});
