import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	clearLastOrgId,
	getLastOrgId,
	LAST_ORG_ID_KEY,
	setLastOrgId,
	createOrganization,
	updateOrganization,
	deleteOrganization,
	leaveOrganization,
	transferOwnership,
	listMembers
} from './api';
import { client } from '$lib/api/client';

describe('lastOrgId storage helpers', () => {
	beforeEach(() => {
		localStorage.clear();
	});

	it('returns null when key is not set', () => {
		expect(getLastOrgId()).toBeNull();
	});

	it('sets and gets last org id', () => {
		setLastOrgId('org-abc-123');
		expect(localStorage.getItem(LAST_ORG_ID_KEY)).toBe('org-abc-123');
		expect(getLastOrgId()).toBe('org-abc-123');
	});

	it('clears last org id', () => {
		setLastOrgId('org-abc-123');
		clearLastOrgId();
		expect(getLastOrgId()).toBeNull();
		expect(localStorage.getItem(LAST_ORG_ID_KEY)).toBeNull();
	});

	it('handles null passed to setLastOrgId', () => {
		setLastOrgId('org-xyz');
		setLastOrgId(null);
		expect(getLastOrgId()).toBeNull();
	});
});

describe('organization API calls', () => {
	it('calls POST /orgs on createOrganization', async () => {
		const spy = vi.spyOn(client, 'POST').mockResolvedValueOnce({
			data: {
				organization: { id: 'org-1', name: 'Acme', slug: 'acme', role: 'owner' }
			},
			response: new Response()
		} as never);

		const res = await createOrganization({ name: 'Acme', slug: 'acme' });
		expect(spy).toHaveBeenCalledWith('/orgs', {
			body: { name: 'Acme', slug: 'acme' }
		});
		expect(res.organization.id).toBe('org-1');
		spy.mockRestore();
	});

	it('calls PATCH /orgs/{orgId} on updateOrganization', async () => {
		const spy = vi.spyOn(client, 'PATCH').mockResolvedValueOnce({
			data: {
				organization: { id: 'org-1', name: 'Acme Updated', slug: 'acme', role: 'owner' }
			},
			response: new Response()
		} as never);

		const res = await updateOrganization('org-1', { name: 'Acme Updated' });
		expect(spy).toHaveBeenCalledWith('/orgs/{orgId}', {
			params: { path: { orgId: 'org-1' } },
			body: { name: 'Acme Updated' }
		});
		expect(res.organization.name).toBe('Acme Updated');
		spy.mockRestore();
	});

	it('calls DELETE /orgs/{orgId} on deleteOrganization', async () => {
		const spy = vi.spyOn(client, 'DELETE').mockResolvedValueOnce({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await deleteOrganization('org-1');
		expect(spy).toHaveBeenCalledWith('/orgs/{orgId}', {
			params: { path: { orgId: 'org-1' } }
		});
		spy.mockRestore();
	});

	it('calls POST /orgs/{orgId}/leave on leaveOrganization', async () => {
		const spy = vi.spyOn(client, 'POST').mockResolvedValueOnce({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await leaveOrganization('org-1');
		expect(spy).toHaveBeenCalledWith('/orgs/{orgId}/leave', {
			params: { path: { orgId: 'org-1' } }
		});
		spy.mockRestore();
	});

	it('calls POST /orgs/{orgId}/transfer-ownership on transferOwnership', async () => {
		const spy = vi.spyOn(client, 'POST').mockResolvedValueOnce({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await transferOwnership('org-1', {
			user_id: 'user-2',
			current_password: 'pass'
		});
		expect(spy).toHaveBeenCalledWith('/orgs/{orgId}/transfer-ownership', {
			params: { path: { orgId: 'org-1' } },
			body: { user_id: 'user-2', current_password: 'pass' }
		});
		spy.mockRestore();
	});

	it('calls GET /orgs/{orgId}/members on listMembers', async () => {
		const spy = vi.spyOn(client, 'GET').mockResolvedValueOnce({
			data: {
				items: [{ user_id: 'user-2', role: 'member', email: 'u2@example.com', name: 'User 2' }]
			},
			response: new Response()
		} as never);

		const res = await listMembers('org-1');
		expect(spy).toHaveBeenCalledWith('/orgs/{orgId}/members', {
			params: { path: { orgId: 'org-1' } }
		});
		expect(res).toHaveLength(1);
		expect(res[0]?.user_id).toBe('user-2');
		spy.mockRestore();
	});
});
