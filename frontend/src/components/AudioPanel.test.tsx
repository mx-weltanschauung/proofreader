import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { usePlayer } from '../audio/playerStore';
import type { AudioRecording, AudioTrack, Chapter, WorkAudio } from '../types';
import { AudioPanel } from './AudioPanel';

const own: Chapter = {
  id: 2,
  work_id: 4,
  title: 'Глава вторая',
  type: 'chapter',
  order_number: 2,
  start_page: 5,
  end_page: 8,
  is_apparatus: false,
  created_at: '',
  updated_at: '',
  children: [],
};
const next: Chapter = { ...own, id: 3, title: 'Глава третья', start_page: 9, end_page: 12 };

const TRACK: AudioTrack = {
  id: 1,
  work_id: 4,
  title: 'Глава вторая',
  start_page: 5,
  end_page: 10,
  duration_ms: 149_902,
  bytes: 10,
  recipe_sha256: 'r',
  pages_sha256: 'p',
  stale: true,
  created_at: '',
  url: '/api/audio/1.opus',
};
const REC: AudioRecording = {
  id: 9,
  work_id: 4,
  chapter_id: 2,
  chapter_title: 'Глава вторая',
  position: 1,
  reader: 'Иванов',
  content_type: 'audio/mpeg',
  bytes: 10,
  duration_ms: 60_000,
  created_at: '',
  url: '/api/audio/rec/9',
};

function renderPanel(
  tracks: AudioTrack[],
  recordings: AudioRecording[],
  volume: WorkAudio = { tracks, recordings },
) {
  return render(
    <MemoryRouter>
      <AudioPanel
        work={{ id: 4, slug: 'kapital-t1', title: 'Капитал, т. 1' }}
        audio={volume}
        chapter={own}
        allChapters={[own, next]}
        tracks={tracks}
        recordings={recordings}
        playlistHref="/api/works/4/chapters/2/download?format=m3u"
      />
    </MemoryRouter>,
  );
}

const playQueue = vi.fn();

beforeEach(() => {
  localStorage.clear();
  usePlayer.setState({ queue: [], index: -1, status: 'idle', position: 0, duration: 0, playQueue });
});
afterEach(() => {
  vi.restoreAllMocks();
  playQueue.mockClear();
});

describe('AudioPanel', () => {
  // Тикет 07, п. 8: склеенная дорожка носит заголовок следующей главы — в
  // панели первой главы читалось «Глава третья / вместе с «Глава третья»».
  it('дорожка названа по соседу — подписана открытой главой, сосед в «вместе с»', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    renderPanel([{ ...TRACK, title: 'Глава третья', stale: false }], []);
    const synth = screen.getByRole('region', { name: 'Синтезированная версия' });
    expect(within(synth).getByText('Глава вторая')).toBeInTheDocument();
    expect(within(synth).queryByText('Глава третья')).toBeNull();
    expect(within(synth).getByText('вместе с «Глава третья»')).toBeInTheDocument();
  });

  it('запись человека сверху, синтез ниже — с пометками и оговоркой; штатных плееров нет', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const { container } = renderPanel([TRACK], [REC]);
    const human = screen.getByRole('region', { name: 'Запись человека' });
    const synth = screen.getByRole('region', { name: 'Синтезированная версия' });
    // Порядок блоков в DOM: запись человека раньше синтеза.
    expect(human.compareDocumentPosition(synth) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(human).getByText('Читает: Иванов')).toBeInTheDocument();
    expect(within(synth).getByText('2 мин 30 с')).toBeInTheDocument();
    expect(within(synth).getByText(/вместе с «Глава третья»/)).toBeInTheDocument();
    expect(
      within(synth).getByText(
        'Текст главы поправлен после озвучки, запись может расходиться с ним',
      ),
    ).toBeInTheDocument();
    expect(container.querySelector('audio')).toBeNull();
    expect(screen.getByText(/Озвучен основной текст/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Подробнее' })).toHaveAttribute('href', '/help#audio');
  });

  // ▶ ставит в очередь дорожки ВСЕГО тома — дальше играет сама, глава за главой.
  it('▶ у дорожки ставит очередь синтеза всего тома с этой дорожки', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const before = { ...TRACK, id: 5, title: 'Глава первая', start_page: 1, end_page: 4 };
    renderPanel([TRACK], [], { tracks: [TRACK, before], recordings: [] });
    await userEvent.click(screen.getByRole('button', { name: 'Слушать: Глава вторая' }));
    const [queue, index] = playQueue.mock.calls[0];
    expect(queue.map((q: { key: string }) => q.key)).toEqual(['track:5', 'track:1']);
    expect(index).toBe(1);
  });

  it('▶ у записи ставит очередь записей тома, без синтеза', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    renderPanel([TRACK], [REC]);
    await userEvent.click(screen.getByRole('button', { name: 'Слушать: запись 1 «Глава вторая»' }));
    const [queue, index] = playQueue.mock.calls[0];
    expect(queue.map((q: { key: string }) => q.key)).toEqual(['rec:9']);
    expect(index).toBe(0);
  });

  // Звук на чужом домене хранилища: без ?download=1 браузер играл бы файл во
  // вкладке, а атрибут download там не действует.
  it('у дорожки и записи — стрелка «скачать» с ?download=1', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    renderPanel([{ ...TRACK, stale: false }], [REC]);
    expect(screen.getByRole('link', { name: 'Скачать «Глава вторая»' })).toHaveAttribute(
      'href',
      '/api/audio/1.opus?download=1',
    );
    expect(screen.getByRole('link', { name: 'Скачать запись 1 «Глава вторая»' })).toHaveAttribute(
      'href',
      '/api/audio/rec/9?download=1',
    );
  });

  // Review Focus 1: iOS старше 18.4 не играет Ogg Opus — у строк синтеза нет
  // ▶, есть совет, ссылка на плейлист и стрелка; mp3 записи человека играет.
  it('без Ogg Opus у синтеза нет ▶ — совет про плейлист и стрелка', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((t: string) =>
      t === 'audio/mpeg' ? 'probably' : '',
    );
    renderPanel([TRACK], [REC]);
    expect(
      screen.getByText(
        'Ваш браузер не воспроизводит этот формат. Скачайте плейлист и откройте его в VLC или другом плеере.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Слушать: Глава вторая' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Скачать «Глава вторая»' })).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Слушать: запись 1 «Глава вторая»' }),
    ).toBeInTheDocument();
    // Совет без ссылки заставлял искать плейлист в меню «Скачать».
    expect(screen.getByRole('link', { name: /Плейлист \.m3u/ })).toHaveAttribute(
      'href',
      '/api/works/4/chapters/2/download?format=m3u',
    );
  });

  // Рецензия: Chrome отвечает '' на 'audio/opus', но играет Ogg Opus — запись
  // .opus спрашивается тем же вопросом, что синтез.
  it('запись .opus играет там, где играет Ogg Opus', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((t: string) =>
      t === 'audio/ogg; codecs=opus' ? 'probably' : '',
    );
    renderPanel([], [{ ...REC, content_type: 'audio/opus' }]);
    expect(
      screen.getByRole('button', { name: 'Слушать: запись 1 «Глава вторая»' }),
    ).toBeInTheDocument();
  });

  it('без записи человека заголовка «Синтезированная версия» нет', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    renderPanel([{ ...TRACK, stale: false, end_page: 8 }], []);
    expect(screen.queryByRole('heading', { name: 'Синтезированная версия' })).toBeNull();
    expect(screen.queryByText(/вместе с/)).toBeNull();
    expect(screen.queryByText(/Текст главы поправлен/)).toBeNull();
  });
});
