import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

vi.mock('../../services/api', () => ({
  usersApi: {
    list: vi.fn().mockResolvedValue({ data: [{ id: 1, email: 'a@x.io', role: 'administrator' }] }),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
}));
import { UsersList } from './UsersList';

describe('UsersList', () => {
  beforeEach(() => vi.clearAllMocks());
  it('renders users from the API', async () => {
    render(
      <MemoryRouter>
        <UsersList />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText('a@x.io')).toBeInTheDocument());
  });
});
