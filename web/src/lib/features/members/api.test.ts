import { beforeEach, describe, expect, it, vi } from 'vitest';
import { changeMemberRole, createMembersQuery, listMembers, removeMember } from './api';
import { client } from '$lib/api/client';
import { createQueryClient } from '$lib/api/query-client';
import { queryKeys } from '$lib/api/query-keys';

describe('members api', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('listMembers calls GET /orgs/{orgId}/members', async () => {
		const mockPage = {
			items: [
				{
					user_id: 'u-1',
					email: 'alice@example.com',
					name: 'Alice',
					avatar_url: null,
					role: 'owner' as const,
					joined_at: '2026-01-01T00:00:00Z'
				}
			],
			next_cursor: 'cursor-2'
		};

		const getSpy = vi.spyOn(client, 'GET').mockResolvedValue({
			data: mockPage,
			response: new Response(JSON.stringify(mockPage), { status: 200 })
		} as never);

		const result = await listMembers('org-1', 'cursor-1', 20);

		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/members', {
			params: {
				path: { orgId: 'org-1' },
				query: { cursor: 'cursor-1', limit: 20 }
			}
		});
		expect(result).toEqual(mockPage);
	});

	it('changeMemberRole calls PATCH /orgs/{orgId}/members/{userId}', async () => {
		const mockResponse = {
			member: {
				user_id: 'u-2',
				email: 'bob@example.com',
				name: 'Bob',
				avatar_url: null,
				role: 'admin' as const,
				joined_at: '2026-01-02T00:00:00Z'
			}
		};

		const patchSpy = vi.spyOn(client, 'PATCH').mockResolvedValue({
			data: mockResponse,
			response: new Response(JSON.stringify(mockResponse), { status: 200 })
		} as never);

		const result = await changeMemberRole('org-1', 'u-2', 'admin');

		expect(patchSpy).toHaveBeenCalledWith('/orgs/{orgId}/members/{userId}', {
			params: {
				path: { orgId: 'org-1', userId: 'u-2' }
			},
			body: { role: 'admin' }
		});
		expect(result).toEqual(mockResponse);
	});

	it('removeMember calls DELETE /orgs/{orgId}/members/{userId}', async () => {
		const deleteSpy = vi.spyOn(client, 'DELETE').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await removeMember('org-1', 'u-2');

		expect(deleteSpy).toHaveBeenCalledWith('/orgs/{orgId}/members/{userId}', {
			params: {
				path: { orgId: 'org-1', userId: 'u-2' }
			}
		});
	});

	it('createMembersQuery configures TanStack infinite query', () => {
		const qc = createQueryClient();
		const query = createMembersQuery(
			() => 'org-42',
			() => qc
		);
		expect(query).toBeDefined();
		expect(queryKeys.members('org-42')).toEqual(['orgs', 'org-42', 'members']);
	});
});
