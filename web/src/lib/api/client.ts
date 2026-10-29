import createClient, { type MiddlewareCallbackParams } from 'openapi-fetch';
import type { paths } from './schema';
import * as publicEnv from '$app/env/public';
import { session, isExpiringSoon } from '$lib/auth/session.svelte';
import { refreshSession } from '$lib/auth/refresh';
import { toApiError } from './errors';

export type SessionEndedHandler = () => void;

let globalOnSessionEnded: SessionEndedHandler | null = null;

export function setOnSessionEnded(handler: SessionEndedHandler | null): void {
	globalOnSessionEnded = handler;
}

export function getOnSessionEnded(): SessionEndedHandler | null {
	return globalOnSessionEnded;
}

export function isPublicPath(path: string, method = 'GET'): boolean {
	const m = method.toUpperCase();

	if (
		path === '/auth/login' ||
		path === '/auth/refresh' ||
		path === '/auth/register' ||
		path === '/auth/verify-email' ||
		path === '/auth/forgot-password' ||
		path === '/auth/reset-password' ||
		path === '/me/email/confirm'
	) {
		return true;
	}

	if (path === '/plans' && m === 'GET') {
		return true;
	}

	if (path === '/invitations/{token}' && m === 'GET') {
		return true;
	}

	if (path === '/invitations/{token}/decline' && m === 'POST') {
		return true;
	}

	// Concrete URL paths
	if (path.startsWith('/invitations/') && !path.endsWith('/accept')) {
		if (path.endsWith('/decline') && m === 'POST') {
			return true;
		}
		if (!path.slice('/invitations/'.length).includes('/') && m === 'GET') {
			return true;
		}
	}

	return false;
}

export interface ApiClientDependencies {
	fetchFn?: typeof fetch;
	apiBase?: string;
	sessionStore?: typeof session;
	isExpiringSoonFn?: () => boolean;
	refreshFn?: () => Promise<boolean>;
}

export function createApiClient(deps: ApiClientDependencies = {}) {
	const apiBase =
		deps.apiBase ??
		(publicEnv as { PUBLIC_API_BASE_URL?: string }).PUBLIC_API_BASE_URL ??
		'/api/v1';

	const client = createClient<paths>({
		baseUrl: apiBase,
		credentials: 'include',
		fetch: deps.fetchFn
	});

	const sessionStore = deps.sessionStore ?? session;
	const isExpiring = deps.isExpiringSoonFn ?? isExpiringSoon;
	const refresh = deps.refreshFn ?? refreshSession;

	client.use({
		onRequest: async ({ request, schemaPath }: MiddlewareCallbackParams) => {
			if (isPublicPath(schemaPath, request.method)) {
				return;
			}

			if (isExpiring()) {
				await refresh();
			}

			if (sessionStore.accessToken) {
				request.headers.set('Authorization', `Bearer ${sessionStore.accessToken}`);
			}
		}
	});

	return client;
}

export const client = createApiClient();

export interface ApiOperationResult<T> {
	data?: T;
	error?: unknown;
	response: Response;
}

export interface CallOptions {
	isPublic?: boolean;
	onSessionEnded?: SessionEndedHandler;
	refreshSession?: () => Promise<boolean>;
}

export async function call<T>(
	operation: () => Promise<ApiOperationResult<T>>,
	options?: CallOptions
): Promise<T> {
	const onSessionEnded = options?.onSessionEnded ?? getOnSessionEnded();
	const refresh = options?.refreshSession ?? refreshSession;

	let res: ApiOperationResult<T>;
	try {
		res = await operation();
	} catch (cause) {
		throw toApiError(null, null, cause);
	}

	if (res.error !== undefined) {
		const apiErr = toApiError(res.response, res.error);

		if (apiErr.code === 'TOKEN_EXPIRED' && res.response.status === 401) {
			const refreshed = await refresh();
			if (refreshed) {
				let retryRes: ApiOperationResult<T>;
				try {
					retryRes = await operation();
				} catch (cause) {
					throw toApiError(null, null, cause);
				}

				if (retryRes.error !== undefined) {
					const retryErr = toApiError(retryRes.response, retryRes.error);
					if (
						retryErr.code === 'TOKEN_EXPIRED' ||
						retryErr.code === 'UNAUTHENTICATED' ||
						retryRes.response.status === 401
					) {
						onSessionEnded?.();
					}
					throw retryErr;
				}

				return retryRes.data as T;
			} else {
				onSessionEnded?.();
				throw apiErr;
			}
		}

		if (apiErr.code === 'UNAUTHENTICATED' && !options?.isPublic) {
			onSessionEnded?.();
		}

		throw apiErr;
	}

	return res.data as T;
}
