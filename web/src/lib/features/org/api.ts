import { createQuery, type CreateQueryResult } from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { Member, OrganizationEnvelope } from '$lib/api/types';

export const LAST_ORG_ID_KEY = 'saka.lastOrgId';

export function getLastOrgId(): string | null {
	try {
		if (typeof window !== 'undefined' && window.localStorage) {
			return window.localStorage.getItem(LAST_ORG_ID_KEY);
		}
	} catch {
		// Ignore storage errors
	}
	return null;
}

export function setLastOrgId(id: string | null): void {
	try {
		if (typeof window !== 'undefined' && window.localStorage) {
			if (id) {
				window.localStorage.setItem(LAST_ORG_ID_KEY, id);
			} else {
				window.localStorage.removeItem(LAST_ORG_ID_KEY);
			}
		}
	} catch {
		// Ignore storage errors
	}
}

export function clearLastOrgId(): void {
	setLastOrgId(null);
}

export async function getOrganization(orgId: string): Promise<OrganizationEnvelope> {
	return call(() => client.GET('/orgs/{orgId}', { params: { path: { orgId } } }));
}

export function createOrganizationQuery(
	orgId: () => string | string
): CreateQueryResult<OrganizationEnvelope, Error> {
	return createQuery(() => {
		const id = typeof orgId === 'function' ? orgId() : orgId;
		return {
			queryKey: queryKeys.organization(id),
			queryFn: () => getOrganization(id),
			enabled: Boolean(id)
		};
	});
}

export async function createOrganization(payload: {
	name: string;
	slug?: string;
}): Promise<OrganizationEnvelope> {
	return call(() =>
		client.POST('/orgs', {
			body: payload
		})
	);
}

export async function updateOrganization(
	orgId: string,
	payload: {
		name?: string;
		slug?: string;
		logo_url?: string | null;
	}
): Promise<OrganizationEnvelope> {
	return call(() =>
		client.PATCH('/orgs/{orgId}', {
			params: { path: { orgId } },
			body: payload
		})
	);
}

export async function deleteOrganization(orgId: string): Promise<void> {
	await call(() =>
		client.DELETE('/orgs/{orgId}', {
			params: { path: { orgId } }
		})
	);
}

export async function leaveOrganization(orgId: string): Promise<void> {
	await call(() =>
		client.POST('/orgs/{orgId}/leave', {
			params: { path: { orgId } }
		})
	);
}

export async function transferOwnership(
	orgId: string,
	payload: {
		user_id: string;
		current_password: string;
	}
): Promise<void> {
	await call(() =>
		client.POST('/orgs/{orgId}/transfer-ownership', {
			params: { path: { orgId } },
			body: payload
		})
	);
}

export async function listMembers(orgId: string): Promise<Member[]> {
	const res = await call(() =>
		client.GET('/orgs/{orgId}/members', {
			params: { path: { orgId } }
		})
	);
	return res.items ?? [];
}

export function createMembersQuery(
	orgId: () => string | string
): CreateQueryResult<Member[], Error> {
	return createQuery(() => {
		const id = typeof orgId === 'function' ? orgId() : orgId;
		return {
			queryKey: queryKeys.members(id),
			queryFn: () => listMembers(id),
			enabled: Boolean(id)
		};
	});
}
