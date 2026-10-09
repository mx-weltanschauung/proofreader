export type UserRole = 'administrator' | 'editor' | 'reader';

export interface AdminUser {
  id: number;
  email: string;
  role: UserRole;
}

/** Подборка читателя глазами администратора: обычная подборка плюс признак
 *  «публиковалась когда-то». Он нужен, чтобы отличить СНЯТУЮ с публикации
 *  (её читатели видели — о ней и жалуются) от черновика (его не видел никто,
 *  кроме автора). Сам признак публикации (`publish_ip_hash`) наружу не
 *  уезжает, поэтому его считает сервер. */
export interface AdminReaderCollection extends Collection {
  was_published: boolean;
}

/** Строка читательского списка администратора. Почты здесь нет, потому что её
 *  у читателя нет вовсе: он входит ником. Число подборок считается по снимку
 *  подписи (collections.author_nickname), а не по владельцу. */
export interface AdminReader {
  id: number;
  nickname: string;
  created_at: string;
  collections_count: number;
}

export type PageStatus =
  | 'не_вычитана'
  | 'вычитывается'
  | 'вычитана'
  | 'есть_проблемы'
  | 'пустая_страница'
  | 'вычитано_машиной'
  | 'требует_внимания';

export type WorkStatus = 'draft' | 'in_progress' | 'completed' | 'archived';

export interface User {
  id: number;
  email: string;
  /** Есть только у читателя — сотрудник входит почтой. */
  nickname?: string;
  role: UserRole;
}

export interface AuthResponse {
  user: User;
  token: string;
  refresh_token: string;
}

export const ARTICLE_KINDS = [
  'статья',
  'рецензия',
  'документ',
  'от_редакции',
  'выступление',
  'прочее',
] as const;
export type ArticleKind = (typeof ARTICLE_KINDS)[number];

export interface ArticleCredit {
  position: number;
  role: 'author' | 'translator';
  printed: string;
  person_id?: number | null;
  person_slug?: string;
}

export interface CreditInput {
  role: 'author' | 'translator';
  printed: string;
  person_id: number | null;
}

export interface Journal {
  id: number;
  slug: string;
  title: string;
  subtitle: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface JournalSummary extends Journal {
  issues_total: number;
  year_from?: number | null;
  year_to?: number | null;
}

export interface JournalIssueRef {
  id: number;
  label: string;
  months: string;
  work_id: number;
  work_slug: string;
}

export interface JournalYear {
  year: number;
  issues: JournalIssueRef[];
}

export interface JournalDetail {
  journal: Journal;
  years: JournalYear[];
}

export interface WorkJournalIssue {
  issue_id: number;
  journal_id: number;
  journal_slug: string;
  journal_title: string;
  year: number;
  label: string;
  months: string;
}

export interface Person {
  id: number;
  name: string;
  sort_key: string;
  slug: string;
}

export interface PersonArticle {
  chapter_id: number;
  chapter_slug: string;
  title: string;
  article_kind: ArticleKind | '';
  role: 'author' | 'translator';
  work_id: number;
  work_slug: string;
  journal_slug: string;
  journal_title: string;
  year: number;
  label: string;
  start_page: number;
  end_page: number;
}

export interface PersonDetail {
  person: Person;
  articles: PersonArticle[];
}

export interface Work {
  /** Журнальные координаты работы-номера; у остальных работ поля нет. */
  journal_issue?: WorkJournalIssue;
  id: number;
  title: string;
  /**
   * Хвост адреса тома (`lenin-t06`). Ключом не является — том ищется по
   * `id` — и может отсутствовать (покрытие полей доводилось постепенно):
   * тогда адрес строится голым номером, что тоже рабочий вид (см.
   * `utils/paths.ts`).
   */
  slug?: string;
  /**
   * Название собрания («К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.»);
   * пусто — работа вне собрания. Считается сервером на чтении, в базе не
   * хранится. Нужно подписи цитаты: автор корпуса живёт здесь, а не в
   * `author` — тот пуст у всех 159 работ.
   */
  edition_title?: string;
  author: string;
  publication_date?: string;
  language: string;
  country: string;
  file_path: string;
  /** Presigned URL for downloading the original file; provided by the API. */
  file_url?: string;
  status: WorkStatus;
  /** 'arabic' у томов, 'roman' у передних листов. */
  numbering_style?: string;
  /** 'volume' | 'front_matter' | 'edition_front_matter'. */
  role?: string;
  parent_work_id?: number;
  /** Служебные работы тома; приходит только с карточки работы. */
  children?: Work[];
  owner_id: number;
  /** Собрание, в которое входит работа; NULL — работа сама по себе. */
  edition_id?: number;
  /** Номер тома внутри собрания; NULL у справочного тома (например указателя). */
  volume_number?: number;
  /** Том, перед которым работа стоит на полке; только у edition_front_matter. */
  precedes_volume?: number;
  /** Полутом I/II/III — существует только у томов 25 и 26. */
  volume_part?: string;
  /** Подпись корешка, заданная руками; пусто — выводится из top_chapters. */
  shelf_label?: string;
  /** Свободный текст на карточку тома; пусто — описания нет. До 2000 знаков. */
  description?: string;
  /** печатная страница = page_number + page_offset */
  page_offset: number;
  created_at: string;
  updated_at: string;
}

export interface Edition {
  id: number;
  title: string;
  slug: string;
  /** Короткий слаг для адресов читальни (`lenin`, `mae`); см. `utils/paths.ts`. */
  url_slug?: string;
  description: string;
  /**
   * Сколько томов в собрании по плану издания; отсутствует, если план
   * неизвестен. Полка пишет по нему «45 из 55»: сколько томов ещё не снято
   * со сканов, из самих данных о работах не выводится никак.
   */
  volumes_planned?: number;
  created_at: string;
  updated_at: string;
}

/** Узел поддерева глав в оглавлении подборки. */
export interface CollectionTocNode {
  /** Адрес главы в томе-источнике: оглавление подборки ведёт по нему в главу. */
  chapter_id: number;
  /** Хвост адреса главы; пуст у главы-нумератора, как и у самой главы. */
  chapter_slug?: string;
  title: string;
  page_start: number;
  children?: CollectionTocNode[];
}

/** Откуда взят элемент. Номера страниц печатные. */
export interface CollectionSource {
  work_id: number;
  /** Хвост адреса тома-источника; пусто, если источник (работа) пропал. */
  work_slug?: string;
  work_title: string;
  edition_title?: string;
  volume_number?: number;
  volume_part?: string;
  page_start: number;
  page_end: number;
}

/** Строка оглавления подборки. */
export interface CollectionEntry {
  id: number;
  kind: 'chapter' | 'work';
  order_number: number;
  title: string;
  /** Автор работы — подсказка рядом с полем переопределения в форме состава. */
  work_author: string;
  author_override: string;
  author: string;
  /** Источник удалён: строка живёт снимком заголовка, читать её нечего. */
  broken: boolean;
  source?: CollectionSource;
  children?: CollectionTocNode[];
}

export interface Collection {
  id: number;
  title: string;
  slug: string;
  description: string;
  owner_id?: number;
  /** Ник читателя-автора; пусто у сотруднической (витринной) подборки —
   *  тот же признак, что и на сервере (Collection.AuthorNickname). Пустая
   *  строка, а не отсутствие поля: сервер всегда его отдаёт. */
  author_nickname: string;
  /** Нет значения — черновик, виден только владельцу. */
  published_at?: string;
  created_at: string;
  updated_at: string;
  /** Заполнено только при чтении одной подборки. */
  items?: CollectionEntry[];
}

/** Строка состава как её возвращает добавление элемента. */
export interface CollectionItem {
  id: number;
  collection_id: number;
  kind: 'chapter' | 'work';
  chapter_id?: number;
  work_id?: number;
  snapshot_title: string;
  snapshot_author: string;
  author_override: string;
  order_number: number;
}

/** Одна из крупнейших работ тома — то, чем один том отличается от другого. */
export interface TopChapter {
  title: string;
  pages: number;
  /** Доля объёма тома, 0..1. */
  share: number;
}

export interface VolumeSummary extends Work {
  pages_total: number;
  /** Ключи — значения PageStatus; статусы без страниц отсутствуют. */
  pages_by_status: Partial<Record<PageStatus, number>>;
  chapters_total: number;
  /** До четырёх крупнейших работ тома, по убыванию объёма; аппарат исключён. */
  top_chapters?: TopChapter[];
}

/**
 * Избранная работа собрания на главной (`edition_highlights`). Подпись пуста —
 * показывается первое предложение заглавия главы (`highlightLabel`).
 */
export interface EditionHighlight {
  chapter_id: number;
  chapter_slug: string;
  chapter_title: string;
  work_id: number;
  work_slug: string;
  volume_number: number | null;
  volume_part: string | null;
  label: string;
}

/** Работа вне собраний, как её показывает главная: имя да ссылка. */
export interface ShelfWork {
  id: number;
  title: string;
  /** Хвост адреса; у работы вне собрания собирается из заголовка. */
  slug?: string;
}

/** Одна полка: собрание со своими томами в порядке издания. */
export interface ShelfEdition {
  edition: Edition;
  volumes: VolumeSummary[];
}

/**
 * Ответ `/api/shelf` — всё, что рисует главная, одним запросом. Прежде она
 * собирала это сама: список собраний и каталог работ, а затем, дождавшись
 * первого, по запросу на каждое собрание за его томами.
 */
export interface Shelf {
  editions: ShelfEdition[];
  loose_works: ShelfWork[];
  /** Необязательно: бэкенд старше журналов поля не шлёт. */
  journals?: JournalSummary[];
}

/**
 * Ответ `/api/static-archive` — текущая сборка статической читальни (вся
 * читальня одним архивом, /help#offline). `url` — прямой адрес файла в
 * бакете, `size` — байты, `date` — день сборки «ГГГГ-ММ-ДД», `works` — число
 * работ каталога в архиве.
 */
export interface StaticArchive {
  file: string;
  url: string;
  size: number;
  sha256: string;
  date: string;
  works: number;
}

export interface Category {
  id: number;
  name: string;
  slug: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface Page {
  id: number;
  work_id: number;
  page_number: number;
  preview_path: string;
  /** Presigned URL for the page preview image; provided by the API. */
  preview_url?: string;
  content_markdown: string;
  status: PageStatus;
  chapter_id?: number;
  created_at: string;
  updated_at: string;
  /**
   * Когда текст полосы правили в последний раз. Приезжает ТОЛЬКО с одиночного
   * чтения полосы (`pagesApi.get`, `pagesApi.getByNumber`); в списках и в
   * карте тома поля нет вовсе — не «не правилась», а «не спрашивали».
   */
  text_edited_at?: string;
}

/** Строка карты страниц тома: всё, чем рисуется обрез. */
export interface PageMapEntry {
  page_number: number;
  status: PageStatus;
}

export interface PageVersion {
  id: number;
  page_id: number;
  content_markdown: string;
  version_number: number;
  user_id: number;
  comment: string;
  created_at: string;
}

export interface Chapter {
  article_kind?: ArticleKind | null;
  credits?: ArticleCredit[];
  id: number;
  work_id: number;
  parent_id?: number | null;
  /** Хвост адреса главы; пуст у главы-нумератора («2», «II»). Не ключ. */
  slug?: string;
  title: string;
  type: string;
  order_number: number;
  start_page: number;
  end_page: number;
  /** Глава — часть аппарата тома (примечания, указатели), а не произведение. */
  is_apparatus: boolean;
  created_at: string;
  updated_at: string;
  children?: Chapter[];
}

/** Судьба черновика относительно модерации. Публикация — отдельная ось
 *  (`published_at`): у опубликованного разбора черновик законно бывает
 *  «на_рассмотрении», и на людях при этом прежняя одобренная редакция.
 *  Постороннему сервер гасит это поле до пустой строки (`documentForViewer`
 *  на сервере) — кухня модерации наружу не едет, поэтому тип включает `''`. */
export type DocumentReviewStatus = 'черновик' | 'на_рассмотрении' | 'одобрено' | 'отклонено';

/** Закрытый список причин отказа: ответить модератору автор не может. */
export type DocumentRejectReason =
  | 'не_по_теме'
  | 'текст_без_разбора'
  | 'брань_или_оскорбления'
  | 'нарушает_закон';

export interface Document {
  id: number;
  /** Ключ адреса ВМЕСТЕ с `author_nickname` (см. `utils/documentPaths.ts`),
   *  не украшение, как `Work.slug`: сервер лепит его из заглавия и замора-
   *  живает при создании, переименование заглавия адрес не двигает. */
  slug: string;
  /** Черновик автора. Постороннему сервер подменяет его одобренной
   *  редакцией (`published_title`), поэтому одно и то же поле означает
   *  разное для разных читателей — и это намеренно: чтобы показать разбор,
   *  клиенту не надо знать, какую редакцию он получил. */
  title: string;
  markdown_content: string;
  // API отдаёт null у разборов без владельца (созданных до появления поля
  // или системным путём) — DocumentView.tsx уже заводит свой локальный тип
  // с этим же уточнением, здесь оно возвращено в общий.
  owner_id: number | null;
  /** Снимок подписи. Пусто — разбор собрала редакция. По нему рисуется
   *  несъёмная пометка «собрал читатель». */
  author_nickname: string;
  /** ОДОБРЕННАЯ редакция — то, что на людях. Сервер отдаёт поле всегда
   *  (без `omitempty` в Go), в т.ч. постороннему: `documentForViewer`
   *  гасит его до `''` в ответе постороннему ПОСЛЕ того, как подставил его
   *  же значение в `title`/`markdown_content` — так что читать его напрямую
   *  клиенту незачем, но поле есть на проводе и не может быть опущено. */
  published_title: string;
  published_markdown: string;
  /** Не null — разбор на людях. */
  published_at: string | null;
  /** Был опубликован хоть раз: по нему снятое отличается от небывшего. */
  was_published: boolean;
  /** Сервер всегда шлёт это поле (без `omitempty`), но гасит его до `''`
   *  постороннему — кухня модерации наружу не едет. Не опционально: поле
   *  присутствует всегда, просто пустой строкой у части читателей. */
  review_status: DocumentReviewStatus | '';
  /** `omitempty` в Go — отсутствует в ответе, а не `null`, пока причины нет
   *  или её гасит `documentForViewer`. */
  reject_reason?: DocumentRejectReason;
  moderator_id?: number;
  reviewed_at?: string;
  submitted_at?: string;
  created_at: string;
  updated_at: string;
}

/** Блок полосы: байтовые границы в markdown и готовый HTML. Обе стороны
 *  произведены одним разрезом на сервере — сопоставлять нечего.
 *
 *  Сервер знает и вид `'footnote'` (`blockKind` на сервере), но блоки этого
 *  вида отфильтрованы до ответа ДО сериализации (`page_blocks.go`, Blocks) —
 *  для них нет вёрстки, тело сноски уезжает в аппарат. В объединение вид
 *  `'footnote'` поэтому НЕ включён: он никогда не приходит по проводу, и
 *  включать его значило бы завести на клиенте ветку без пары. */
export interface PageBlock {
  start: number;
  end: number;
  kind: 'paragraph' | 'heading' | 'quote' | 'list';
  html: string;
}

/** Вклейка вставленного в разбор фрагмента корпуса. `status: 'stale'` —
 *  якорь не нашёлся после правки полосы; текста уже не показать, ссылка на
 *  том ещё жива (см. renderCutState на сервере). */
export interface DocumentCut {
  id: number;
  document_id: number;
  work_id: number | null;
  status: 'ok' | 'stale';
  source_title: string;
}

export interface NoteIndexEntry {
  number: number;
  target_page: number;
  target_page_id: number;
  body_html: string;
}

/**
 * Тело PUT /api/works/{id} в части координат тома.
 *
 * Сервер различает отсутствующий ключ и явный null: отсутствие сохраняет
 * прежнее значение, null — очищает (см. parseVolumeUpdate в
 * internal/api/work_volume.go). В `Work` эти поля необязательные, то есть
 * `number | undefined`, а undefined в JSON просто исчезает — очистить им
 * ничего нельзя. Поэтому у payload'а свой тип.
 */
export type WorkUpdatePayload = Partial<
  Omit<Work, 'edition_id' | 'volume_number' | 'volume_part'>
> & {
  edition_id?: number | null;
  volume_number?: number | null;
  volume_part?: string | null;
};

export type ConceptKind = 'article' | 'redirect';

/**
 * Понятие в списке навигатора. Полей одной статьи (адрес скана, текст,
 * издание) здесь нет намеренно: список каталожный, понятие может нести
 * несколько статей, и общего «единственного» адреса/текста у него больше
 * нет — список их и не рисует (`ConceptList.tsx` читает только `kind`).
 */
export interface ConceptSummary {
  id: number;
  title: string;
  slug: string;
  sort_key: string;
  kind: ConceptKind;
  created_at: string;
  updated_at: string;
}

/**
 * Адрес в томе. `page_start`/`page_end` — ПЕЧАТНЫЕ номера; `work_id` и
 * `page_number` приходят только при `resolved`, то есть когда том привязан к
 * собранию и страница у него есть.
 */
export interface ConceptReference {
  id: number;
  article_id: number;
  volume_number: number;
  volume_part?: string;
  page_start: number;
  page_end: number;
  rubric: string;
  /**
   * Путь подрубрик от корня к листу (задача 8/9). Нет поля или пустой массив
   * — адрес висит прямо на статье; длина 1 — обычная плоская подрубрика,
   * `rubric` дублирует единственный элемент; длина 2 — вложенность вроде
   * «I съезд РСДРП…» → «значение съезда». Запрос на сервере рекурсивный, то
   * есть глубина не зашита в схему — код, читающий это поле, не должен
   * полагаться на длину 1 или 2 как на потолок.
   */
  rubric_path?: string[];
  order_number: number;
  is_uncertain: boolean;
  note?: string;
  resolved: boolean;
  work_id?: number;
  page_number?: number;
  /** Хвост адреса тома-цели; приходит вместе с work_id, при resolved. */
  work_slug?: string;
}

export type ConceptLinkKind = 'see' | 'see_also';

/** Отсылка к другому понятию. `target_slug` пуст, если цель ещё не разобрана. */
export interface ConceptLink {
  id: number;
  /**
   * from_article_id, не from_concept_id: миграция 000026 (задача 11) сняла
   * from_concept_id с index_concept_links — отсылка теперь принадлежит
   * СТАТЬЕ (index_concept_articles), а не понятию целиком, у него их может
   * быть несколько. API этого поля больше не отдаёт; раунд правок 1,
   * находка 8 — прежний тип объявлял его как обязательное присутствующее.
   */
  from_article_id: number;
  to_concept_id?: number;
  target_title: string;
  kind: ConceptLinkKind;
  order_number: number;
  target_slug?: string;
}

/**
 * Статья одного указателя о понятии: издание, свой адрес скана, свой текст,
 * свои адреса и отсылки. `work_id` пуст у статьи из указателя, взятого с
 * внешнего источника (тогда скана нет, и ссылки на источник читальня не
 * даёт вовсе — `source_url` не показывается, у ленинского указателя пуст).
 */
export interface ConceptArticle {
  id: number;
  edition_id: number;
  edition_title: string;
  work_id?: number;
  source_url: string;
  title: string;
  kind: ConceptKind;
  article_markdown: string;
  source_page_start: number;
  source_page_end: number;
  references: ConceptReference[];
  links: ConceptLink[];
}

/**
 * Понятие каталога: одна карточка предметного указателя, раскрытая одной или
 * несколькими статьями — каждая от своего собрания.
 */
export interface Concept {
  id: number;
  slug: string;
  title: string;
  sort_key: string;
  created_at: string;
  updated_at: string;
  articles: ConceptArticle[];
  /** Может отсутствовать у старых ответов сервера — читать как пустой список. */
  incoming_links?: ConceptIncomingLink[] | null;
}

/** Отсылка, ведущая НА понятие: печатная статья показывает только исходящие. */
export interface ConceptIncomingLink {
  slug: string;
  title: string;
  kind: ConceptLinkKind;
}

/** Одна страничная часть вырезки: кусок текста одной страницы. */
export interface ConceptCutPart {
  page_id: number;
  page_number: number;
  printed_page: number;
  page_status: PageStatus;
  html: string;
}

/**
 * Границы настоящей вырезки в байтах: те же четыре числа, что хранит
 * IndexFragment на сервере. Страницы — идентификаторы БД, не номера тома.
 */
export interface ConceptCutBounds {
  start_page_id: number;
  start_offset: number;
  end_page_id: number;
  end_offset: number;
}

/**
 * Вырезка адреса. `id` пуст у синтетической — той, что сервер подставляет
 * вместо ненарезанного или отвязавшегося адреса, чтобы форма была одна.
 * `bounds` согласован с `id`: есть один — есть другой, у синтетической
 * вырезки границ не существует, и они равны `null`, а не выдуманным нулям.
 */
export interface ConceptCut {
  id: number | null;
  bounds: ConceptCutBounds | null;
  status: string;
  /**
   * Головная цитата-якорь: из неё собирается внешний адрес места
   * (`?quote=`). Пусто у синтетической вырезки — якоря у неё нет, и внешней
   * ссылки на место тоже.
   */
  head_quote: string;
  parts: ConceptCutPart[];
}

/** Страница адреса — и та, что попала в вырезку, и та, что нет. */
export interface ConceptEntryPage {
  page_id: number;
  page_number: number;
  printed_page: number;
  page_status: PageStatus;
}

/** Запись потока — один адрес указателя со своей подрубрикой. */
export interface ConceptEntry {
  reference_id: number;
  volume_number: number;
  volume_part?: string;
  printed_start: number;
  printed_end: number;
  work_id: number;
  work_title: string;
  /** Хвост адреса тома; см. SearchChapter.work_slug. */
  work_slug?: string;
  chapter_title: string;
  /**
   * id той же главы, что и chapter_title, — шапка записи делает из пары
   * ссылку. null — страница адреса вне всех глав.
   */
  chapter_id: number | null;
  chapter_slug?: string;
  rubric: string;
  /**
   * Путь подрубрик от корня к листу; `rubric` рядом остаётся листом. Поле
   * ОБЯЗАТЕЛЬНОЕ, не опциональное, и по той же причине, что `pages`:
   * фикстура теста, где его забыли, должна падать на типах, а не молча
   * рисовать поток, в котором съезды слиты в один блок. Сервер всегда шлёт
   * массив, пустой при адресе без подрубрики.
   */
  rubric_path: string[];
  is_uncertain: boolean;
  /** fragment — есть вырезки; whole_page — нет; stale — отвязались. */
  state: 'fragment' | 'whole_page' | 'stale';
  /**
   * ВСЕ страницы адреса, в порядке чтения, включая те, которых не коснулась
   * ни одна вырезка: `cuts[].parts[]` знает только покрытые, а без page_id
   * ненарезанную страницу нечем открыть — разворот адресуется по
   * идентификатору. Поле обязательное, не опциональное: фикстура теста, где
   * его забыли, должна падать на типах, а не молча рисовать запись, в
   * которой половина адреса не существует.
   */
  pages: ConceptEntryPage[];
  cuts: ConceptCut[];
  /**
   * Вырезки адреса, отвязавшиеся от текста при переякоривании — id и
   * границы без частей (`parts` всегда пуст). Никогда не показываются как
   * текст, но должны попасть обратно в тело PUT .../cuts при следующем
   * сохранении: сервер заменяет набор адреса целиком, и вырезка, забытая и
   * тут, и в `cuts`, будет стёрта первым же сохранением после правки другой
   * страницы того же адреса.
   */
  stale_cuts: ConceptCut[];
}

/**
 * Порядок чтения потока понятия: по подрубрикам (как устроена печатная
 * статья) или по томам и страницам (сквозное чтение).
 */
export type ConceptOrder = 'rubric' | 'page';

/** Порция потока. `total` — все адреса после фильтра, не размер порции. */
export interface ConceptEntriesPage {
  total: number;
  entries: ConceptEntry[];
}

/** Страница, развёрнутая под записью: куски с пометкой «внутри вырезки». */
export interface ConceptExpandedPage {
  page_id: number;
  page_number: number;
  printed_page: number;
  page_status: PageStatus;
  markdown: string;
  chunks: { html: string; inside: boolean }[];
}

/**
 * Ссылка на нетронутую вырезку этого же адреса вместо пересказа её границами.
 * Сервер переносит вырезку с этим id как есть — границы, цитаты, хеши,
 * статус, — не читая для неё ничего больше из запроса. Единственный способ
 * сохранить вырезку с исчезнувшими частями (`ConceptCut.parts: []`): у такой
 * на клиенте нет номеров страниц, чтобы описать её границами, а id есть
 * всегда.
 */
export interface ConceptCutByID {
  id: number;
}

/**
 * Границы вырезки в запросе на запись: смещения БАЙТОВЫЕ. `status`
 * необязателен — пустой наследует статус уровня запроса (обратная
 * совместимость с нарезчиком, шлющим один статус на всю пачку); редактор
 * границ проставляет его на каждую вырезку, чтобы не подтвердить чужую
 * машинную вырезку тем, что человек поправил соседнюю.
 */
export interface ConceptCutByBounds {
  start_page: number;
  start_offset: number;
  end_page: number;
  end_offset: number;
  status?: 'machine' | 'confirmed';
}

/** Один элемент тела `PUT .../cuts` — либо ссылка по id, либо границы. */
export type ConceptCutInput = ConceptCutByID | ConceptCutByBounds;

/** Понятие, описываемое на конкретной странице тома. */
export interface ConceptBacklink {
  concept_id: number;
  slug: string;
  title: string;
  rubric: string;
  rubric_path?: string[];
  page_start: number;
  page_end: number;
}

/**
 * Одна полоса главы или элемента подборки: вёрстка и бит «пустая».
 *
 * Ни markdown-источника, ни id, ни статуса: полная модель страницы везла бы
 * рядом с вёрсткой тот же текст ещё раз и удваивала сырой ответ.
 */
export interface ChapterPage {
  page_number: number;
  html: string;
  /** Пустая полоса: маркер номера ей не рисуют, иначе он сядет на номер соседа. */
  blank: boolean;
}

/** Одна страница окна потокового чтения. */
export interface ReadingPage {
  page_number: number;
  html: string;
  /**
   * Сноски этой страницы. Постранично, как в книге: у потока нет области,
   * по которой можно было бы вести сквозной счёт, — он не кончается.
   */
  notes_html: string;
  /** Пустая полоса: маркер номера ей не рисуют, иначе он сядет на номер соседа. */
  blank: boolean;
}

/** Окно страниц работы и указание, откуда брать следующее. */
export interface ReadingWindow {
  pages: ReadingPage[];
  /** Первая страница следующего окна; null — работа кончилась. */
  next_from: number | null;
  total_pages: number;
}

export type SuggestionStatus = 'новое' | 'принято' | 'отклонено';

export type RejectReason = 'так_в_оригинале' | 'уже_исправлено' | 'не_по_теме';

/** Строка списка: без текстов — их возит только разбор одного предложения. */
export interface SuggestionRow {
  id: number;
  page_id: number;
  work_id: number;
  work_title: string;
  page_number: number;
  page_offset: number;
  note: string;
  status: SuggestionStatus;
  reject_reason?: RejectReason;
  stale: boolean;
  length_delta: number;
  created_at: string;
  resolved_at?: string;
}

export interface SuggestionDetail extends SuggestionRow {
  base_markdown: string;
  proposed_markdown: string;
  current_markdown: string;
}

/** Обращение — письмо читателя в читальню. */
export interface Feedback {
  id: number;
  message: string;
  source_path: string;
  /** null — письмо не разобрано. */
  handled_at: string | null;
  created_at: string;
}

/** Глава, совпавшая названием (ответ /api/search). */
export interface SearchChapter {
  id: number;
  title: string;
  /** Хвосты адреса главы и её тома; см. utils/paths.ts. */
  slug?: string;
  work_slug?: string;
  work_id: number;
  work_title: string;
  /** Подпись тома на полке; у работы вне собрания пустая. */
  volume_label: string;
  edition_title: string;
  is_apparatus: boolean;
}

export interface SearchConcept {
  slug: string;
  title: string;
}

/**
 * Том со счётом совпавших полос. У служебной работы (role front_matter)
 * подпись, собрание и parent_work_id — родительские: список рисует её под
 * родителем с пометкой роли.
 */
export interface SearchVolume {
  work_id: number;
  /** Хвост адреса тома; см. SearchChapter.work_slug. */
  work_slug?: string;
  title: string;
  author: string;
  volume_label: string;
  edition_id: number | null;
  edition_title: string;
  role: string;
  parent_work_id: number | null;
  text_hits: number;
  apparatus_hits: number;
  /** arabic | roman — печатную колонцифру полос тома клиент считает сам. */
  numbering_style: string;
  /**
   * Первые несколько совпавших полос тома в порядке чтения. Не окно списка:
   * за остальными читатель идёт по ссылке «все N полос» в режим ?work=.
   */
  pages: SearchPage[];
}

/** Первый экран поиска. Списки сервер гарантирует непустыми ([]), не null. */
export interface SearchResponse {
  query: string;
  /** Леммы запроса (snowball, без исключённых) — префиксы слов для подсветки. */
  terms: string[];
  chapters: SearchChapter[];
  concepts: SearchConcept[];
  volumes: SearchVolume[];
  total_hits: number;
}

export interface SearchTermsResponse {
  query: string;
  terms: string[];
}

/**
 * Совпавшая полоса. В snippet границы совпадения — U+0001/U+0002, HTML нет:
 * рисуется через splitSnippet текстовыми узлами и <mark>.
 */
export interface SearchPage {
  page_number: number;
  printed_number: number;
  chapter_title: string | null;
  /** id той же главы, что и chapter_title — выдача делает из пары ссылку. */
  chapter_id: number | null;
  is_apparatus: boolean;
  snippet: string;
}

/** Глава тома с числом совпавших полос — строка списка «где нашлось». */
export interface SearchChapterFacet {
  id: number;
  title: string;
  hits: number;
}

export interface SearchPagesResponse {
  query: string;
  terms: string[];
  total: number;
  pages: SearchPage[];
  /** До 30 глав по убыванию попаданий; считается по всему тому. */
  chapters: SearchChapterFacet[];
  /** Сколько всего глав тома содержат попадания. */
  chapters_total: number;
}

/** Сводка о файловом кэше глав (GET /api/cache/stats, только администратор). */
export interface CacheStats {
  enabled: boolean;
  files: number;
  bytes: number;
  oldest_age_seconds: number;
}

// Посещаемость и здоровье (/api/admin/stats/*). Поле day приходит как RFC3339 —
// это ключ группировки, а не дата для показа.
export interface StatsTrafficRow {
  day: string;
  channel: string;
  views: number;
  visitors: number;
}
export interface StatsTopRow {
  work_id: number;
  entity_id: number;
  slug_key: string;
  title: string;
  work_title: string;
  missing: boolean;
  views: number;
  visitors: number;
}
export interface StatsCountRow {
  key: string;
  views: number;
  visitors: number;
}
export interface StatsSearchRow {
  query: string;
  count: number;
  zero_hits: number;
}
export interface StatsSearches {
  frequent: StatsSearchRow[];
  zero: StatsSearchRow[];
}
export interface StatsCrawlerRow {
  day: string;
  agent: string;
  views: number;
}
export interface StatsDeviceRow {
  day: string;
  device: '' | 'narrow' | 'medium' | 'wide';
  views: number;
  visitors: number;
}
export interface StatsRouteHealth {
  route: string;
  requests: number;
  status_5xx: number;
  status_503: number;
  p50_ms: number;
  p95_ms: number;
}
export interface StatsHealth {
  started_at: string;
  routes: StatsRouteHealth[];
  values: Record<string, number>;
}
export type StatsTopKind = 'work' | 'chapter' | 'document' | 'collection' | 'concept' | 'edition';

/** Статусы заявки на озвучку — русские строки, как у полос (models.Audio*). */
export type AudioQueueStatus = 'в_очереди' | 'синтезируется' | 'готово' | 'ошибка';

/** Синтезированная дорожка: диапазон полос тома. url — относительный
 *  (`/api/audio/{id}.opus`, 302 на подписанную ссылку). */
export interface AudioTrack {
  id: number;
  work_id: number;
  title: string;
  start_page: number;
  end_page: number;
  duration_ms: number;
  bytes: number;
  recipe_sha256: string;
  pages_sha256: string;
  stale: boolean;
  created_at: string;
  url: string;
}

/** Запись человека, прикреплённая к главе. Диапазона главы в ответе нет —
 *  главы ищутся по chapter_id в дереве тома. */
export interface AudioRecording {
  id: number;
  work_id: number;
  chapter_id: number;
  chapter_title: string;
  position: number;
  reader: string;
  content_type: string;
  bytes: number;
  duration_ms: number;
  created_at: string;
  url: string;
}

/** GET /works/{id}/audio. */
export interface WorkAudio {
  tracks: AudioTrack[];
  recordings: AudioRecording[];
}

export interface AudioQueueItem {
  id: number;
  work_id: number;
  work_title: string;
  /** null — заявка на весь том. */
  chapter_id: number | null;
  chapter_title: string;
  status: AudioQueueStatus;
  error: string;
  status_counts: Record<string, number>;
  requested_by: string;
  requested_at: string;
  claimed_at: string | null;
  finished_at: string | null;
}

export interface AudioStaleWork {
  work_id: number;
  work_title: string;
  stale: number;
}

/** GET /audio/queue: незавершённые и упавшие заявки плюс сводка устаревших. */
export interface AudioQueue {
  items: AudioQueueItem[];
  stale: AudioStaleWork[];
}

/** POST …/recordings/uploads: ключ и подписанная ссылка на PUT. */
export interface RecordingUpload {
  key: string;
  url: string;
  content_type: string;
}
