import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useAuth } from './useAuth';
import { readLaterApi } from '../services/api';
import type { ReadLaterTarget } from '../types';

export function useReadLater(userId: number, openShelf = false) {
  const { token } = useAuth();
  return useQuery({
    queryKey: ['read-later', userId],
    queryFn: async ({ signal }) => (await readLaterApi.list(token, signal)).data,
    staleTime: 30_000,
    refetchOnMount: openShelf ? 'always' : true,
    refetchOnWindowFocus: 'always',
  });
}

export function useChangeReadLater(userId: number, target: ReadLaterTarget) {
  const { token } = useAuth();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (saved: boolean) => readLaterApi.change(token, target, saved),
    onSuccess: () => client.invalidateQueries({ queryKey: ['read-later', userId] }),
  });
}
