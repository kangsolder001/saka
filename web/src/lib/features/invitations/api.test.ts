import { describe, expect, it, vi, beforeEach } from 'vitest';
import { acceptInvitation, declineInvitation, getInvitation } from './api';
import { client } from '$lib/api/client';

describe('invitations api', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('getInvitation calls GET /invitations/{token}', async () => {
		const mockInvitation = {
			email: 'invitee@example.com',
			role: 'member' as const,
			organization: {
				name: 'Acme',
				logo_url: null
			},
			inviter_name: 'Admin',
			expires_at: '2026-02-01T00:00:00Z',
			account_exists: false
		};

		vi.spyOn(client, 'GET').mockResolvedValue({
			data: { invitation: mockInvitation },
			response: new Response(JSON.stringify({ invitation: mockInvitation }), { status: 200 })
		} as never);

		const result = await getInvitation('token-123');
		expect(result).toEqual(mockInvitation);
	});

	it('acceptInvitation calls POST /invitations/{token}/accept', async () => {
		const mockEnvelope = {
			organization: {
				id: 'org-1',
				name: 'Acme',
				slug: 'acme',
				logo_url: null,
				created_at: '2026-01-01T00:00:00Z'
			}
		};

		vi.spyOn(client, 'POST').mockResolvedValue({
			data: mockEnvelope,
			response: new Response(JSON.stringify(mockEnvelope), { status: 200 })
		} as never);

		const result = await acceptInvitation('token-123');
		expect(result).toEqual(mockEnvelope);
	});

	it('declineInvitation calls POST /invitations/{token}/decline', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await declineInvitation('token-123');
		expect(postSpy).toHaveBeenCalledWith('/invitations/{token}/decline', {
			params: { path: { token: 'token-123' } }
		});
	});
});
