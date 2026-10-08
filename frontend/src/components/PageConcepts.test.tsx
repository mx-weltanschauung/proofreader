import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { conceptsApi } from '../services/api';
import type { ConceptBacklink } from '../types';
import { PageConcepts } from './PageConcepts';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function backlink(over: Partial<ConceptBacklink>): ConceptBacklink {
  return {
    concept_id: 1,
    slug: 'abstrakciya',
    title: 'Абстракция, абстрактное и конкретное',
    rubric: 'и действительность',
    page_start: 85,
    page_end: 85,
    ...over,
  };
}

function renderConcepts() {
  return render(
    <MemoryRouter>
      <PageConcepts workId={4} pageId={1001} />
    </MemoryRouter>,
  );
}

describe('PageConcepts', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('собирает подрубрики одного понятия под одним заголовком', async () => {
    // Записи приходят по ссылкам, поэтому понятие встречается несколько раз
    // с разными подрубриками — на экране это одна запись, а не три.
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() =>
      ok([
        backlink({ concept_id: 1, rubric: 'и действительность' }),
        backlink({ concept_id: 1, rubric: 'как приём исследования' }),
        backlink({ concept_id: 2, slug: 'trud', title: 'Труд', rubric: 'определение' }),
      ]),
    );

    renderConcepts();

    expect(await screen.findByText(/Понятия указателя \(2\)/)).toBeInTheDocument();
    expect(
      screen.getAllByRole('link', { name: 'Абстракция, абстрактное и конкретное' }),
    ).toHaveLength(1);
    expect(screen.getByText(/и действительность/)).toBeInTheDocument();
    expect(screen.getByText(/как приём исследования/)).toBeInTheDocument();
  });

  it('ведёт на страницу понятия', async () => {
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok([backlink({})]));

    renderConcepts();

    expect(await screen.findByRole('link', { name: /Абстракция/ })).toHaveAttribute(
      'href',
      '/concepts/abstrakciya',
    );
  });

  it('на пустом списке не рисует ничего', async () => {
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok([]));

    const { container } = renderConcepts();

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(container).toBeEmptyDOMElement();
  });

  it('о сбое говорит вслух', async () => {
    // Иначе поломка выглядит ровно как «понятий на странице нет».
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => Promise.reject(new Error('boom')));
    vi.spyOn(console, 'error').mockImplementation(() => {});

    renderConcepts();

    expect(await screen.findByText(/Не удалось загрузить понятия/)).toBeInTheDocument();
  });

  it('показывает диапазон, когда он есть', async () => {
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() =>
      ok([backlink({ page_start: 46, page_end: 47 })]),
    );

    renderConcepts();

    expect(await screen.findByText(/46—47/)).toBeInTheDocument();
  });

  it('различает подрубрики с одинаковым листом по родительскому пути', async () => {
    // Задача 9 слила семь съездов в одно понятие «КПСС — съезды» — без пути
    // все семь строк читались бы как одна и та же «значение съезда».
    const congresses = Array.from({ length: 7 }, (_, i) =>
      backlink({
        concept_id: 1,
        rubric: 'значение съезда',
        rubric_path: [`${i + 1} съезд РСДРП`, 'значение съезда'],
        page_start: 10 + i,
        page_end: 10 + i,
      }),
    );
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(congresses));

    renderConcepts();

    expect(await screen.findByText(/Понятия указателя \(1\)/)).toBeInTheDocument();
    for (let i = 1; i <= 7; i++) {
      expect(
        screen.getByText(new RegExp(`${i} съезд РСДРП — значение съезда`)),
      ).toBeInTheDocument();
    }
  });

  it('без пути (или пути в одно звено) подрубрика рисуется как раньше — голым листом', async () => {
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() =>
      ok([backlink({ rubric: 'определение', rubric_path: ['определение'] })]),
    );

    renderConcepts();

    expect(await screen.findByText('определение · 85')).toBeInTheDocument();
  });

  it('показывает первые пять понятий и раскрывает остальные по кнопке', async () => {
    const many = Array.from({ length: 9 }, (_, i) =>
      backlink({ concept_id: i + 1, slug: `c${i + 1}`, title: `Понятие ${i + 1}` }),
    );
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(many));

    renderConcepts();

    // Заголовок считает ВСЕ понятия полосы, а не только показанные —
    // потолок его не касается ни до, ни после раскрытия.
    expect(await screen.findByText('Понятия указателя (9)')).toBeInTheDocument();
    expect(await screen.findAllByRole('link')).toHaveLength(5);
    await userEvent.click(screen.getByRole('button', { name: 'ещё 4' }));
    expect(screen.getAllByRole('link')).toHaveLength(9);
    expect(screen.getByText('Понятия указателя (9)')).toBeInTheDocument();
  });

  it('не рисует кнопку раскрытия, когда понятий не больше потолка', async () => {
    const five = Array.from({ length: 5 }, (_, i) =>
      backlink({ concept_id: i + 1, slug: `c${i + 1}`, title: `Понятие ${i + 1}` }),
    );
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(five));

    renderConcepts();

    expect(await screen.findAllByRole('link')).toHaveLength(5);
    expect(screen.queryByRole('button', { name: /^ещё/ })).not.toBeInTheDocument();
  });

  it('сбрасывает раскрытие при переходе на другую полосу', async () => {
    const many = Array.from({ length: 9 }, (_, i) =>
      backlink({ concept_id: i + 1, slug: `c${i + 1}`, title: `Понятие ${i + 1}` }),
    );
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(many));

    const { rerender } = render(
      <MemoryRouter>
        <PageConcepts workId={4} pageId={1001} />
      </MemoryRouter>,
    );

    expect(await screen.findAllByRole('link')).toHaveLength(5);
    await userEvent.click(screen.getByRole('button', { name: 'ещё 4' }));
    expect(screen.getAllByRole('link')).toHaveLength(9);

    // Тот же компонент, другая полоса — без перемонтирования, только
    // сменой пропсов, как при листании тома.
    rerender(
      <MemoryRouter>
        <PageConcepts workId={4} pageId={1002} />
      </MemoryRouter>,
    );

    expect(await screen.findAllByRole('link')).toHaveLength(5);
    expect(screen.queryByRole('button', { name: 'ещё 4' })).toBeInTheDocument();
  });
});
