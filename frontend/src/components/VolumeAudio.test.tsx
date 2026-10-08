import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { usePlayer } from '../audio/playerStore';
import { audioApi } from '../services/api';
import type { AudioRecording, AudioTrack, Work, WorkAudio } from '../types';
import { VolumeAudio } from './VolumeAudio';

vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as AxiosResponse<T>);
}

const WORK = { id: 4, title: 'Том 4' } as Work;

const T = (id: number, stale = false): AudioTrack => ({
  id,
  work_id: 4,
  title: `Дорожка ${id}`,
  start_page: id,
  end_page: id,
  duration_ms: 60_000,
  bytes: 1,
  recipe_sha256: 'r',
  pages_sha256: 'p',
  stale,
  created_at: '',
  url: `/api/audio/${id}.opus`,
});
const R: AudioRecording = {
  id: 9,
  work_id: 4,
  chapter_id: 223,
  chapter_title: 'Вопрос о прусском банке',
  position: 1,
  reader: 'Иванов',
  content_type: 'audio/mpeg',
  bytes: 1,
  duration_ms: 1000,
  created_at: '',
  url: '/api/audio/rec/9',
};

function renderAudio(
  audio: WorkAudio,
  editable = false,
  onChanged = vi.fn().mockResolvedValue(undefined),
) {
  return render(
    <MemoryRouter>
      <VolumeAudio
        work={WORK}
        audio={audio}
        chapters={[]}
        editable={editable}
        onChanged={onChanged}
      />
    </MemoryRouter>,
  );
}

afterEach(() => vi.restoreAllMocks());

describe('VolumeAudio', () => {
  it('читателю: дорожки тома, записи со значком, плейлист и оговорка', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    renderAudio({ tracks: [T(1), T(2, true)], recordings: [R] });
    const section = screen.getByRole('region', { name: 'Аудио' });
    expect(within(section).getByText('запись человека')).toBeInTheDocument();
    expect(within(section).getByText(/Вопрос о прусском банке/)).toBeInTheDocument();
    expect(
      within(section).getAllByText(
        'Текст главы поправлен после озвучки, запись может расходиться с ним',
      ),
    ).toHaveLength(1);
    expect(within(section).getByRole('link', { name: /Плейлист \.m3u/ })).toHaveAttribute(
      'href',
      '/api/works/4/download?format=m3u',
    );
    expect(within(section).getByText(/Озвучен основной текст/)).toBeInTheDocument();
    // У читателя кнопки — только ▶ строк; кнопок сотрудника нет.
    expect(within(section).queryByRole('button', { name: 'Поставить весь том' })).toBeNull();
    expect(within(section).getByRole('link', { name: 'Скачать «Дорожка 2»' })).toHaveAttribute(
      'href',
      '/api/audio/2.opus?download=1',
    );
    expect(
      within(section).getByRole('link', { name: 'Скачать запись 1 «Вопрос о прусском банке»' }),
    ).toHaveAttribute('href', '/api/audio/rec/9?download=1');
    expect(section.querySelector('audio')).toBeNull();
  });

  it('▶ у дорожки ставит очередь синтеза тома с этой дорожки', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const playQueue = vi.fn();
    usePlayer.setState({ queue: [], index: -1, status: 'idle', playQueue });
    renderAudio({ tracks: [T(2), T(1)], recordings: [R] });
    await userEvent.click(screen.getByRole('button', { name: 'Слушать: Дорожка 2' }));
    const [queue, index] = playQueue.mock.calls[0];
    expect(queue.map((q: { key: string }) => q.key)).toEqual(['track:1', 'track:2']);
    expect(index).toBe(1);
  });

  it('без Ogg Opus — совет про плейлист вместо плееров синтеза', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('');
    renderAudio({ tracks: [T(1)], recordings: [] });
    expect(screen.getByText(/Ваш браузер не воспроизводит этот формат/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Слушать: Дорожка 1' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Скачать «Дорожка 1»' })).toBeInTheDocument();
  });

  it('запись .opus играет там, где играет Ogg Opus', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((t: string) =>
      t === 'audio/ogg; codecs=opus' ? 'probably' : '',
    );
    renderAudio({ tracks: [], recordings: [{ ...R, content_type: 'audio/opus' }] });
    expect(
      screen.getByRole('button', { name: 'Слушать: запись 1 «Вопрос о прусском банке»' }),
    ).toBeInTheDocument();
  });

  it('сотруднику: «Поставить весь том» и «Устарело N дорожек — поставить заново»', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const enqueue = vi.spyOn(audioApi, 'enqueue').mockImplementation(() => ok({} as never));
    const requeue = vi
      .spyOn(audioApi, 'requeueStale')
      .mockImplementation(() => ok({ queued: 1, items: [] }));
    const onChanged = vi.fn().mockResolvedValue(undefined);
    renderAudio({ tracks: [T(1, true), T(2, true), T(3)], recordings: [] }, true, onChanged);
    await userEvent.click(screen.getByRole('button', { name: 'Поставить весь том' }));
    expect(enqueue).toHaveBeenCalledWith(4);
    await userEvent.click(
      screen.getByRole('button', { name: 'Устарело 2 дорожки — поставить заново' }),
    );
    expect(requeue).toHaveBeenCalledWith(4);
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(2));
  });

  it('сотруднику без устаревших — второй кнопки нет', () => {
    renderAudio({ tracks: [T(1)], recordings: [] }, true);
    expect(screen.queryByRole('button', { name: /Устарел/ })).toBeNull();
  });
});
