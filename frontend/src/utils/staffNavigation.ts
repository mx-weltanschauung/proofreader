export type StaffCount = 'suggestions' | 'comments' | 'feedback';

type StaffLink = {
  to: string;
  label: string;
  count?: StaffCount;
  adminOnly?: boolean;
};

type StaffGroup = {
  id: string;
  title: string;
  adminOnly?: boolean;
  links: StaffLink[];
};

export const STAFF_GROUPS: StaffGroup[] = [
  {
    id: 'frequent',
    title: 'Часто используемые',
    links: [
      { to: '/suggestions/queue', label: 'Предложения', count: 'suggestions' },
      { to: '/admin/readers', label: 'Читатели', adminOnly: true },
      { to: '/admin/stats', label: 'Статистика', adminOnly: true },
    ],
  },
  {
    id: 'work',
    title: 'Работа',
    links: [
      { to: '/documents/review', label: 'Разборы на проверку' },
      { to: '/reader-comments/review', label: 'Комментарии', count: 'comments' },
      { to: '/admin/audio', label: 'Озвучка' },
    ],
  },
  {
    id: 'management',
    title: 'Управление',
    adminOnly: true,
    links: [
      { to: '/admin/users', label: 'Пользователи' },
      { to: '/admin/feedback', label: 'Обращения', count: 'feedback' },
      { to: '/admin/closures', label: 'Закрытия' },
      { to: '/admin/cache', label: 'Кэш' },
      { to: '/admin/settings', label: 'Настройки' },
    ],
  },
  {
    id: 'mine',
    title: 'Моё',
    links: [
      { to: '/mine/notes', label: 'Заметки' },
      { to: '/mine/comments', label: 'Мои комментарии' },
    ],
  },
];

export function staffGroups(admin: boolean): StaffGroup[] {
  return STAFF_GROUPS.filter((group) => admin || !group.adminOnly).map((group) => ({
    ...group,
    links: group.links.filter((link) => admin || !link.adminOnly),
  }));
}
