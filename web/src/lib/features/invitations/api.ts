import {
	createInfiniteQuery,
	type CreateInfiniteQueryResult,
	type InfiniteData,
	type QueryClient
} from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { operations } from '$lib/api/schema';
import type { Invitation, InvitationInfo, OrganizationEnvelope } from '$lib/api/types';

export type ListInvitationsResponse =
	operations['listInvitations']['responses']['200']['content']['application/json'];

export interface CreateInvitationResult {
	invitation: Invitation;
	status: number;
}

export async function getInvitation(token: string): Promise<InvitationInfo> {
	const res = await call(() =>
		client.GET('/invitations/{token}', {
			params: {
				path: { token }
			}
		})
	);
	return res.invitation;
}

export async function acceptInvitation(token: string): Promise<OrganizationEnvelope> {
	return call(() =>
		client.POST('/invitations/{token}/accept', {
			params: {
				path: { token }
			}
		})
	);
}

export async function declineInvitation(token: string): Promise<void> {
	await call(() =>
		client.POST('/invitations/{token}/decline', {
			params: {
				path: { token }
			}
		})
	);
}

export async function listInvitations(
	orgId: string,
	cursor?: string,
	limit?: number
): Promise<ListInvitationsResponse> {
	return call(() =>
		client.GET('/orgs/{orgId}/invitations', {
			params: {
				path: { orgId },
				query: {
					...(cursor ? { cursor } : {}),
					...(limit !== undefined ? { limit } : {})
				}
			}
		})
	);
}

export function createInvitationsQuery(
	orgId: () => string,
	queryClient?: () => QueryClient
): CreateInfiniteQueryResult<InfiniteData<ListInvitationsResponse>, Error> {
	return createInfiniteQuery(() => {
		const id = orgId();
		return {
			queryKey: queryKeys.invitations(id),
			queryFn: ({ pageParam }) => listInvitations(id, pageParam as string | undefined),
			initialPageParam: undefined as string | undefined,
			getNextPageParam: (last) => last.next_cursor ?? undefined,
			enabled: Boolean(id)
		};
	}, queryClient);
}

export async function createInvitation(
	orgId: string,
	payload: { email: string; role: 'admin' | 'member' }
): Promise<CreateInvitationResult> {
	let responseStatus = 201;
	const res = await call(async () => {
		const r = await client.POST('/orgs/{orgId}/invitations', {
			params: { path: { orgId } },
			body: payload
		});
		if (r.response?.status) {
			responseStatus = r.response.status;
		}
		return r;
	});
	return { invitation: res.invitation, status: responseStatus };
}

export async function resendInvitation(
	orgId: string,
	id: string
): Promise<{ invitation: Invitation }> {
	return call(() =>
		client.POST('/orgs/{orgId}/invitations/{id}/resend', {
			params: {
				path: { orgId, id }
			}
		})
	);
}

export async function revokeInvitation(orgId: string, id: string): Promise<void> {
	await call(() =>
		client.DELETE('/orgs/{orgId}/invitations/{id}', {
			params: {
				path: { orgId, id }
			}
		})
	);
}
