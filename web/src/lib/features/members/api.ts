import {
	createInfiniteQuery,
	type CreateInfiniteQueryResult,
	type InfiniteData,
	type QueryClient
} from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { operations } from '$lib/api/schema';
import type { Member } from '$lib/api/types';

export type ListMembersResponse =
	operations['listMembers']['responses']['200']['content']['application/json'];

export async function listMembers(
	orgId: string,
	cursor?: string,
	limit?: number
): Promise<ListMembersResponse> {
	return call(() =>
		client.GET('/orgs/{orgId}/members', {
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

export function createMembersQuery(
	orgId: () => string,
	queryClient?: () => QueryClient
): CreateInfiniteQueryResult<InfiniteData<ListMembersResponse>, Error> {
	return createInfiniteQuery(() => {
		const id = orgId();
		return {
			queryKey: queryKeys.members(id),
			queryFn: ({ pageParam }) => listMembers(id, pageParam as string | undefined),
			initialPageParam: undefined as string | undefined,
			getNextPageParam: (last) => last.next_cursor ?? undefined,
			enabled: Boolean(id)
		};
	}, queryClient);
}

export async function changeMemberRole(
	orgId: string,
	userId: string,
	role: 'admin' | 'member'
): Promise<{ member: Member }> {
	return call(() =>
		client.PATCH('/orgs/{orgId}/members/{userId}', {
			params: {
				path: { orgId, userId }
			},
			body: { role }
		})
	);
}

export async function removeMember(orgId: string, userId: string): Promise<void> {
	await call(() =>
		client.DELETE('/orgs/{orgId}/members/{userId}', {
			params: {
				path: { orgId, userId }
			}
		})
	);
}
