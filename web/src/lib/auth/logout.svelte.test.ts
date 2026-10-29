import { describe, expect, it, vi, beforeEach } from 'vitest';
import { logout } from './logout';
import { session, setSession } from './session.svelte';
import { client } from '$lib/api/client';
import { queryClient } from '$lib/api/query-client';
import * as navigation from '$app/navigation';

vi.mock('$app/navigation', () => ({
	goto: vi.fn().mockResolvedValue(undefined)
}));

describe('logout', () => {
	beforeEach(() => {
		setSession({
			access_token: 'test-token',
			token_type: 'Bearer',
			expires_in: 900,
			user: { id: 'u1', name: 'User 1', email: 'u1@example.com', email_verified: true }
		});
	});

	it('clears session and query cache, posts broadcast and navigates to /login', async () => {
		const postSpy = vi
			.spyOn(client, 'POST')
			.mockResolvedValue({} as Awaited<ReturnType<typeof client.POST>>);
		const clearSpy = vi.spyOn(queryClient, 'clear');

		await logout();

		expect(postSpy).toHaveBeenCalledWith('/auth/logout');
		expect(session.status).toBe('anonymous');
		expect(session.accessToken).toBeNull();
		expect(clearSpy).toHaveBeenCalled();
		expect(navigation.goto).toHaveBeenCalledWith('/login');
	});
});
