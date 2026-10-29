import { createQuery, type CreateQueryResult } from '@tanstack/svelte-query';
import { call, client } from '$lib/api/client';
import { queryKeys } from '$lib/api/query-keys';
import type { operations } from '$lib/api/schema';
import type { Profile, Session } from '$lib/api/types';

export type GetMeResponse = operations['getMe']['responses']['200']['content']['application/json'];
export type UpdateMeResponse =
	operations['updateMe']['responses']['200']['content']['application/json'];
export type ListSessionsResponse =
	operations['listSessions']['responses']['200']['content']['application/json'];

export async function getMe(): Promise<GetMeResponse> {
	return call(() => client.GET('/me'));
}

export function createMeQuery(): CreateQueryResult<GetMeResponse, Error> {
	return createQuery(() => ({
		queryKey: queryKeys.me,
		queryFn: getMe
	}));
}

export interface UpdateMeParams {
	name?: string;
	avatar_url?: string | null;
}

export async function updateMe(params: UpdateMeParams): Promise<{ user: Profile }> {
	return call(() =>
		client.PATCH('/me', {
			body: params
		})
	);
}

export async function changePassword(
	currentPassword: string,
	newPassword: string
): Promise<{ message: string }> {
	return call(() =>
		client.POST('/me/password', {
			body: {
				current_password: currentPassword,
				new_password: newPassword
			}
		})
	);
}

export async function requestEmailChange(
	newEmail: string,
	currentPassword: string
): Promise<{ message: string }> {
	return call(() =>
		client.POST('/me/email', {
			body: {
				new_email: newEmail,
				current_password: currentPassword
			}
		})
	);
}

export async function listSessions(): Promise<Session[]> {
	const res = await call(() => client.GET('/me/sessions'));
	return res.sessions;
}

export function createSessionsQuery(): CreateQueryResult<Session[], Error> {
	return createQuery(() => ({
		queryKey: queryKeys.sessions,
		queryFn: listSessions
	}));
}

export async function revokeSession(sessionId: string): Promise<void> {
	await call(() =>
		client.DELETE('/me/sessions/{id}', {
			params: {
				path: { id: sessionId }
			}
		})
	);
}

export async function deleteMe(currentPassword: string): Promise<void> {
	await call(() =>
		client.DELETE('/me', {
			body: {
				current_password: currentPassword
			}
		})
	);
}
