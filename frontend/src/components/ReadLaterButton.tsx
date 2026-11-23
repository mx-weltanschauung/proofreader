import { useContext } from 'react';
import { QueryClientContext } from '@tanstack/react-query';
import toast from 'react-hot-toast';
import { useAuth } from '../hooks/useAuth';
import { useChangeReadLater, useReadLater } from '../hooks/useReadLater';
import type { ReadLaterTarget } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import './ReadLaterButton.css';

type Props = {
  target: ReadLaterTarget;
  title: string;
  compact?: boolean;
};

export function ReadLaterButton(props: Props) {
  const { isAuthenticated, user } = useAuth();
  const client = useContext(QueryClientContext);
  if (!isAuthenticated || !user || !client) return null;

  return (
    <Toggle
      key={`${user.id}/${props.target.kind}/${props.target.id}`}
      {...props}
      userId={user.id}
    />
  );
}

function Toggle({ target, title, compact, userId }: Props & { userId: number }) {
  const shelf = useReadLater(userId);
  const change = useChangeReadLater(userId, target);
  const saved =
    shelf.data?.some((entry) =>
      target.kind === 'chapter'
        ? entry.chapter_id === target.id
        : entry.chapter_id === undefined && entry.work_id === target.id,
    ) ?? false;
  const label = saved ? 'Убрать из отложенного' : 'Отложить';

  return (
    <button
      type="button"
      className={compact ? 'read-later-compact' : 'btn btn-secondary'}
      title={label}
      aria-label={`${label}: ${title}`}
      aria-pressed={saved}
      disabled={shelf.isPending || change.isPending}
      onClick={() =>
        change.mutate(!saved, {
          onError: (error) => toast.error(apiErrorMessage(error, 'Не удалось обновить полку')),
        })
      }
    >
      {saved ? 'Отложено' : 'Отложить'}
    </button>
  );
}
