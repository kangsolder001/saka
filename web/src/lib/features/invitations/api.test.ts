import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	acceptInvitation,
	createInvitation,
	createInvitationsQuery,
	declineInvitation,
	getInvitation,
	listInvitations,
	resendInvitation,
	revokeInvitation
} from './api';
import { client } from '$lib/api/client';
import { createQueryClient } from '$lib/api/query-client';
import { queryKeys } from '$lib/api/query-keys';

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

	it('listInvitations calls GET /orgs/{orgId}/invitations', async () => {
		const mockPage = {
			items: [
				{
					id: 'inv-1',
					email: 'pending@example.com',
					role: 'member' as const,
					invited_by: 'u-1',
					expires_at: '2026-02-01T00:00:00Z',
					created_at: '2026-01-25T00:00:00Z'
				}
			],
			next_cursor: null
		};

		const getSpy = vi.spyOn(client, 'GET').mockResolvedValue({
			data: mockPage,
			response: new Response(JSON.stringify(mockPage), { status: 200 })
		} as never);

		const result = await listInvitations('org-1', 'cursor-1', 10);
		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/invitations', {
			params: {
				path: { orgId: 'org-1' },
				query: { cursor: 'cursor-1', limit: 10 }
			}
		});
		expect(result).toEqual(mockPage);
	});

	it('createInvitation calls POST /orgs/{orgId}/invitations and returns 201 for new invitation', async () => {
		const mockInvitation = {
			id: 'inv-new',
			email: 'new@example.com',
			role: 'member' as const,
			invited_by: 'u-1',
			expires_at: '2026-02-01T00:00:00Z',
			created_at: '2026-01-25T00:00:00Z'
		};

		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { invitation: mockInvitation },
			response: new Response(JSON.stringify({ invitation: mockInvitation }), { status: 201 })
		} as never);

		const result = await createInvitation('org-1', { email: 'new@example.com', role: 'member' });
		expect(postSpy).toHaveBeenCalledWith('/orgs/{orgId}/invitations', {
			params: { path: { orgId: 'org-1' } },
			body: { email: 'new@example.com', role: 'member' }
		});
		expect(result).toEqual({ invitation: mockInvitation, status: 201 });
	});

	it('createInvitation returns 200 when an invitation is refreshed', async () => {
		const mockInvitation = {
			id: 'inv-existing',
			email: 'existing@example.com',
			role: 'admin' as const,
			invited_by: 'u-1',
			expires_at: '2026-02-08T00:00:00Z',
			created_at: '2026-01-25T00:00:00Z'
		};

		vi.spyOn(client, 'POST').mockResolvedValue({
			data: { invitation: mockInvitation },
			response: new Response(JSON.stringify({ invitation: mockInvitation }), { status: 200 })
		} as never);

		const result = await createInvitation('org-1', {
			email: 'existing@example.com',
			role: 'admin'
		});
		expect(result).toEqual({ invitation: mockInvitation, status: 200 });
	});

	it('resendInvitation calls POST /orgs/{orgId}/invitations/{id}/resend', async () => {
		const mockInvitation = {
			id: 'inv-1',
			email: 'user@example.com',
			role: 'member' as const,
			invited_by: 'u-1',
			expires_at: '2026-02-08T00:00:00Z',
			created_at: '2026-01-25T00:00:00Z'
		};

		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { invitation: mockInvitation },
			response: new Response(JSON.stringify({ invitation: mockInvitation }), { status: 200 })
		} as never);

		const result = await resendInvitation('org-1', 'inv-1');
		expect(postSpy).toHaveBeenCalledWith('/orgs/{orgId}/invitations/{id}/resend', {
			params: { path: { orgId: 'org-1', id: 'inv-1' } }
		});
		expect(result).toEqual({ invitation: mockInvitation });
	});

	it('revokeInvitation calls DELETE /orgs/{orgId}/invitations/{id}', async () => {
		const deleteSpy = vi.spyOn(client, 'DELETE').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await revokeInvitation('org-1', 'inv-1');
		expect(deleteSpy).toHaveBeenCalledWith('/orgs/{orgId}/invitations/{id}', {
			params: { path: { orgId: 'org-1', id: 'inv-1' } }
		});
	});

	it('createInvitationsQuery configures infinite query', () => {
		const qc = createQueryClient();
		const query = createInvitationsQuery(
			() => 'org-99',
			() => qc
		);
		expect(query).toBeDefined();
		expect(queryKeys.invitations('org-99')).toEqual(['orgs', 'org-99', 'invitations']);
	});
});
