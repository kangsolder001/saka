import { createQuery, type CreateQueryResult } from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { OrganizationEnvelope } from '$lib/api/types';

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
