import {
	createQuery,
	createInfiniteQuery,
	type CreateQueryResult,
	type CreateInfiniteQueryResult,
	type InfiniteData,
	type QueryClient
} from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryClient as defaultQueryClient } from '$lib/api/query-client';
import { queryKeys } from '$lib/api/query-keys';
import type {
	Plan,
	Subscription,
	Invoice,
	CheckoutResponse,
	ChangePlanResponse
} from '$lib/api/types';

export async function getPlans(): Promise<Plan[]> {
	const res = await call(() => client.GET('/plans'));
	return (res as { items: Plan[] }).items ?? [];
}

export function createPlansQuery(
	queryClient?: () => QueryClient
): CreateQueryResult<Plan[], Error> {
	return createQuery(
		() => ({
			queryKey: queryKeys.plans,
			queryFn: () => getPlans()
		}),
		queryClient
	);
}

export async function getSubscription(orgId: string): Promise<Subscription> {
	const res = await call(() =>
		client.GET('/orgs/{orgId}/subscription', {
			params: { path: { orgId } }
		})
	);
	return (res as { subscription: Subscription }).subscription;
}

export function createSubscriptionQuery(
	orgId: (() => string) | string,
	queryClient?: () => QueryClient
): CreateQueryResult<Subscription, Error> {
	return createQuery(
		() => {
			const id = typeof orgId === 'function' ? orgId() : orgId;
			return {
				queryKey: queryKeys.subscription(id),
				queryFn: () => getSubscription(id),
				enabled: Boolean(id)
			};
		},
		queryClient ?? (() => defaultQueryClient)
	);
}

export interface ListInvoicesParams {
	limit?: number;
	cursor?: string;
}

export interface ListInvoicesResponse {
	items: Invoice[];
	next_cursor?: string;
}

export async function listInvoices(
	orgId: string,
	options?: ListInvoicesParams | string,
	maybeLimit?: number
): Promise<ListInvoicesResponse> {
	let cursor: string | undefined;
	let limit: number | undefined;

	if (typeof options === 'string') {
		cursor = options;
		limit = maybeLimit ?? 20;
	} else if (options && typeof options === 'object') {
		cursor = options.cursor;
		limit = options.limit;
	}

	const res = await call(() =>
		client.GET('/orgs/{orgId}/invoices', {
			params: {
				path: { orgId },
				query: {
					...(cursor ? { cursor } : {}),
					...(limit !== undefined ? { limit } : {})
				}
			}
		})
	);
	return {
		items: (res as { items?: Invoice[] }).items ?? [],
		next_cursor: (res as { next_cursor?: string }).next_cursor
	};
}

export function createInvoicesQuery(
	orgId: () => string,
	queryClient?: () => QueryClient
): CreateInfiniteQueryResult<InfiniteData<ListInvoicesResponse>, Error> {
	return createInfiniteQuery(() => {
		const id = orgId();
		return {
			queryKey: queryKeys.invoices(id),
			queryFn: ({ pageParam }) => listInvoices(id, { cursor: pageParam as string | undefined }),
			initialPageParam: undefined as string | undefined,
			getNextPageParam: (last) => last.next_cursor ?? undefined,
			enabled: Boolean(id)
		};
	}, queryClient);
}

export const createInfiniteInvoicesQuery = createInvoicesQuery;

export async function getInvoice(orgId: string, invoiceId: string): Promise<Invoice> {
	const res = await call(() =>
		client.GET('/orgs/{orgId}/invoices/{id}', {
			params: { path: { orgId, id: invoiceId } }
		})
	);
	return (res as { invoice: Invoice }).invoice;
}

export function createInvoiceQuery(
	orgId: (() => string) | string,
	invoiceId: (() => string) | string,
	queryClient?: () => QueryClient
): CreateQueryResult<Invoice, Error> {
	return createQuery(() => {
		const id = typeof orgId === 'function' ? orgId() : orgId;
		const invId = typeof invoiceId === 'function' ? invoiceId() : invoiceId;
		return {
			queryKey: queryKeys.invoice(id, invId),
			queryFn: () => getInvoice(id, invId),
			enabled: Boolean(id && invId)
		};
	}, queryClient);
}

export async function checkout(
	orgId: string,
	payload: { plan: string } | string,
	qc: QueryClient = defaultQueryClient
): Promise<CheckoutResponse> {
	const body = typeof payload === 'string' ? { plan: payload } : payload;
	const res = await call(() =>
		client.POST('/orgs/{orgId}/subscription/checkout', {
			params: { path: { orgId } },
			body
		})
	);
	await Promise.all([
		qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) }),
		qc.invalidateQueries({ queryKey: queryKeys.invoices(orgId) })
	]);
	return res as CheckoutResponse;
}

export async function changePlan(
	orgId: string,
	payload: { plan: string } | string,
	qc: QueryClient = defaultQueryClient
): Promise<ChangePlanResponse> {
	const body = typeof payload === 'string' ? { plan: payload } : payload;
	const res = await call(() =>
		client.POST('/orgs/{orgId}/subscription/change-plan', {
			params: { path: { orgId } },
			body
		})
	);
	await Promise.all([
		qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) }),
		qc.invalidateQueries({ queryKey: queryKeys.invoices(orgId) })
	]);
	return res as ChangePlanResponse;
}

export async function cancelSubscription(
	orgId: string,
	qc: QueryClient = defaultQueryClient
): Promise<Subscription> {
	const res = await call(() =>
		client.POST('/orgs/{orgId}/subscription/cancel', {
			params: { path: { orgId } }
		})
	);
	await Promise.all([
		qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) }),
		qc.invalidateQueries({ queryKey: queryKeys.invoices(orgId) })
	]);
	return (res as { subscription: Subscription }).subscription;
}

export async function resumeSubscription(
	orgId: string,
	qc: QueryClient = defaultQueryClient
): Promise<Subscription> {
	const res = await call(() =>
		client.POST('/orgs/{orgId}/subscription/resume', {
			params: { path: { orgId } }
		})
	);
	await Promise.all([
		qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) }),
		qc.invalidateQueries({ queryKey: queryKeys.invoices(orgId) })
	]);
	return (res as { subscription: Subscription }).subscription;
}
