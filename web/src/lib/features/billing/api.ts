import { createQuery, type CreateQueryResult } from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { Subscription, Invoice } from '$lib/api/types';

export async function getSubscription(orgId: string): Promise<Subscription> {
	const res = await call(() =>
		client.GET('/orgs/{orgId}/subscription', {
			params: { path: { orgId } }
		})
	);
	return (res as { subscription: Subscription }).subscription;
}

export function createSubscriptionQuery(
	orgId: () => string
): CreateQueryResult<Subscription, Error> {
	return createQuery(() => {
		const id = orgId();
		return {
			queryKey: queryKeys.subscription(id),
			queryFn: () => getSubscription(id),
			enabled: Boolean(id)
		};
	});
}

export async function listInvoices(
	orgId: string,
	cursor?: string,
	limit = 20
): Promise<{ items: Invoice[]; next_cursor?: string }> {
	const res = await call(() =>
		client.GET('/orgs/{orgId}/invoices', {
			params: {
				path: { orgId },
				query: cursor ? { cursor, limit } : { limit }
			}
		})
	);
	return res as { items: Invoice[]; next_cursor?: string };
}

export function createInvoicesQuery(
	orgId: () => string
): CreateQueryResult<{ items: Invoice[]; next_cursor?: string }, Error> {
	return createQuery(() => {
		const id = orgId();
		return {
			queryKey: queryKeys.invoices(id),
			queryFn: () => listInvoices(id),
			enabled: Boolean(id)
		};
	});
}
