import { createQuery, type CreateQueryResult } from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { operations } from '$lib/api/schema';

export type GetMeResponse = operations['getMe']['responses']['200']['content']['application/json'];

export async function getMe(): Promise<GetMeResponse> {
	return call(() => client.GET('/me'));
}

export function createMeQuery(): CreateQueryResult<GetMeResponse, Error> {
	return createQuery(() => ({
		queryKey: queryKeys.me,
		queryFn: getMe
	}));
}
