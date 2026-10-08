import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { AxiosResponse } from 'axios';
import { audioApi } from '../services/api';
import type { AudioQueue, AudioQueueItem, AudioTrack, Chapter } from '../types';
import { AudioQueueControl } from './AudioQueueControl';

vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as AxiosResponse<T>);
}

const CH: Chapter = {
  id: 223,
  work_id: 4,
  title: 'Вопрос о прусском банке',
  type: 'chapter',
  order_number: 1,
  start_page: 22,
  end_page: 23,
  is_apparatus: false,
  created_at: '',
  updated_at: '',
};

function item(id: number, status: AudioQueueItem['status']): AudioQueueItem {
  return {
    id,
    work_id: 4,
    work_title: 'Том 4',
    chapter_id: 223,
    chapter_title: CH.title,
    status,
    error: status === 'ошибка' ? 'упало' : '',
    status_counts: {},
    requested_by: '',
    requested_at: '',
    claimed_at: null,
    finished_at: null,
  };
}

function queue(items: AudioQueueItem[]) {
  vi.spyOn(audioApi, 'queue').mockImplementation(() => ok<AudioQueue>({ items, stale: [] }));
}

const STALE: AudioTrack = {
  id: 1,
  work_id: 4,
  title: '',
  start_page: 22,
  end_page: 23,
  duration_ms: 1,
  bytes: 1,
  recipe_sha256: 'r',
  pages_sha256: 'p',
  stale: true,
  created_at: '',
  url: '',
};

afterEach(() => vi.restoreAllMocks());

describe('AudioQueueControl', () => {
  it('без заявок — «Поставить в озвучку» ставит главу и перечитывает', async () => {
    queue([]);
    const enqueue = vi
      .spyOn(audioApi, 'enqueue')
      .mockImplementation(() => ok(item(1, 'в_очереди')));
    const onChanged = vi.fn().mockResolvedValue(undefined);
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[]}
        onChanged={onChanged}
      />,
    );
    await userEvent.click(await screen.findByRole('button', { name: 'Поставить в озвучку' }));
    expect(enqueue).toHaveBeenCalledWith(4, 223);
    await waitFor(() => expect(audioApi.queue).toHaveBeenCalledTimes(2));
    expect(onChanged).toHaveBeenCalled();
  });

  it('в очереди — надпись и «снять»', async () => {
    queue([item(7, 'в_очереди')]);
    const cancel = vi.spyOn(audioApi, 'cancel').mockImplementation(() => ok(undefined));
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[]}
        onChanged={vi.fn()}
      />,
    );
    expect(await screen.findByText('В очереди на озвучку')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'снять' }));
    expect(cancel).toHaveBeenCalledWith(7);
  });

  it('синтезируется — только надпись, снять нельзя', async () => {
    queue([item(7, 'синтезируется')]);
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[]}
        onChanged={vi.fn()}
      />,
    );
    expect(await screen.findByText('Озвучивается')).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('ошибка — «Озвучка не удалась — повторить»', async () => {
    queue([item(7, 'ошибка')]);
    const retry = vi.spyOn(audioApi, 'retry').mockImplementation(() => ok(item(7, 'в_очереди')));
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[]}
        onChanged={vi.fn()}
      />,
    );
    await userEvent.click(
      await screen.findByRole('button', { name: 'Озвучка не удалась — повторить' }),
    );
    expect(retry).toHaveBeenCalledWith(7);
  });

  // Тикет 07, п. 5: текст ошибки жил только в title — на сенсорном экране его
  // не увидеть. Раскрывается касанием «почему?».
  it('ошибка — текст причины открывается касанием, а не только в title', async () => {
    queue([item(7, 'ошибка')]);
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[]}
        onChanged={vi.fn()}
      />,
    );
    await userEvent.click(await screen.findByText('почему?'));
    expect(screen.getByText('упало')).toBeVisible();
  });

  // Рецензия, п. 2: упавшая заявка иначе заслоняет звук главы навсегда.
  it('ошибку можно снять — заявка уходит, глава снова «Поставить в озвучку»', async () => {
    const q = vi
      .spyOn(audioApi, 'queue')
      .mockImplementationOnce(() => ok<AudioQueue>({ items: [item(7, 'ошибка')], stale: [] }))
      .mockImplementation(() => ok<AudioQueue>({ items: [], stale: [] }));
    const cancel = vi.spyOn(audioApi, 'cancel').mockImplementation(() => ok(undefined));
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[]}
        onChanged={vi.fn()}
      />,
    );
    await userEvent.click(await screen.findByRole('button', { name: 'снять' }));
    expect(cancel).toHaveBeenCalledWith(7);
    expect(await screen.findByRole('button', { name: 'Поставить в озвучку' })).toBeInTheDocument();
    expect(q).toHaveBeenCalledTimes(2);
  });

  it('устаревшая дорожка — «Озвучка устарела — поставить заново»', async () => {
    queue([]);
    const enqueue = vi
      .spyOn(audioApi, 'enqueue')
      .mockImplementation(() => ok(item(1, 'в_очереди')));
    render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus={false}
        chapterTracks={[STALE]}
        onChanged={vi.fn()}
      />,
    );
    await userEvent.click(
      await screen.findByRole('button', { name: 'Озвучка устарела — поставить заново' }),
    );
    expect(enqueue).toHaveBeenCalledWith(4, 223);
  });

  // Review Focus 4: аппарат не озвучивается — кнопки нет, очередь не читается.
  it('глава-аппарат — ничего', () => {
    const q = vi.spyOn(audioApi, 'queue');
    const { container } = render(
      <AudioQueueControl
        workId={4}
        chapter={CH}
        apparatus
        chapterTracks={[]}
        onChanged={vi.fn()}
      />,
    );
    expect(container).toBeEmptyDOMElement();
    expect(q).not.toHaveBeenCalled();
  });

  // Тикет 07, п. 6: признак наследуется по дереву тома, а оно приходит позже
  // очереди — кнопка, уже нарисованная, обязана уйти.
  it('аппарат выяснился после очереди — кнопка уходит', async () => {
    queue([]);
    const props = { workId: 4, chapter: CH, chapterTracks: [], onChanged: vi.fn() };
    const { container, rerender } = render(<AudioQueueControl {...props} apparatus={false} />);
    await screen.findByRole('button', { name: 'Поставить в озвучку' });
    rerender(<AudioQueueControl {...props} apparatus />);
    expect(container).toBeEmptyDOMElement();
  });
});
