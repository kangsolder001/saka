import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from 'vitest-browser-svelte';
import AcceptInvitationPage from './+page.svelte';
import { client } from '$lib/api/client';
import { session } from '$lib/auth/session.svelte';

vi.mock('$app/state', () => ({
	page: {
		url: new URL('http://localhost:3000/accept-invitation?token=valid-token')
	}
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

describe('AcceptInvitationPage', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		session.status = 'anonymous';
		session.user = null;
	});

	it('renders invitation details for unauthenticated user with account_exists: false', async () => {
		const mockInvitation = {
			email: 'newuser@example.com',
			role: 'member' as const,
			organization: {
				name: 'Acme Space',
				logo_url: null
			},
			inviter_name: 'Captain Kirk',
			expires_at: '2026-12-31T23:59:59Z',
			account_exists: false
		};

		vi.spyOn(client, 'GET').mockResolvedValue({
			data: { invitation: mockInvitation },
			response: new Response(JSON.stringify({ invitation: mockInvitation }), { status: 200 })
		} as never);

		const screen = render(AcceptInvitationPage);

		await vi.waitFor(() => {
			expect(screen.getByText('Acme Space')).toBeDefined();
			expect(
				screen.getByRole('button', { name: /Sign up to accept|Daftar untuk menerima/i })
			).toBeDefined();
			expect(
				screen.getByRole('button', { name: /Decline invitation|Tolak Undangan/i })
			).toBeDefined();
		});
	});

	it('renders accept button for authenticated user with matching email', async () => {
		session.status = 'authenticated';
		session.user = {
			id: 'u1',
			name: 'Existing Member',
			email: 'existing@example.com',
			email_verified: true
		};

		const mockInvitation = {
			email: 'existing@example.com',
			role: 'admin' as const,
			organization: {
				name: 'Enterprise Fleet',
				logo_url: null
			},
			inviter_name: 'Spock',
			expires_at: '2026-12-31T23:59:59Z',
			account_exists: true
		};

		vi.spyOn(client, 'GET').mockResolvedValue({
			data: { invitation: mockInvitation },
			response: new Response(JSON.stringify({ invitation: mockInvitation }), { status: 200 })
		} as never);

		const screen = render(AcceptInvitationPage);

		await vi.waitFor(() => {
			expect(screen.getByText('Enterprise Fleet')).toBeDefined();
			expect(
				screen.getByRole('button', { name: /Accept invitation|Terima Undangan/i })
			).toBeDefined();
			expect(
				screen.getByRole('button', { name: /Decline invitation|Tolak Undangan/i })
			).toBeDefined();
		});
	});
});
