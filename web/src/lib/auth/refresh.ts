import * as publicEnv from '$app/env/public';
import { setSession, clearSession } from './session.svelte';
import type { TokenResponse } from '$lib/api/types';

export interface RefresherDependencies {
	fetchFn?: typeof fetch;
	locks?: LockManager | null;
	apiBase?: string;
	onSetSession?: (res: TokenResponse) => void;
	onClearSession?: () => void;
}

export function createRefresher(deps: RefresherDependencies = {}) {
	let inFlightRefresh: Promise<boolean> | null = null;

	const executeRefresh = async (): Promise<boolean> => {
		const fetchFn = deps.fetchFn ?? fetch;
		const apiBase =
			deps.apiBase ??
			(publicEnv as { PUBLIC_API_BASE_URL?: string }).PUBLIC_API_BASE_URL ??
			'/api/v1';
		const onSetSession = deps.onSetSession ?? setSession;
		const onClearSession = deps.onClearSession ?? clearSession;

		try {
			const res = await fetchFn(`${apiBase}/auth/refresh`, {
				method: 'POST',
				credentials: 'include'
			});

			if (res.ok) {
				const data = (await res.json()) as TokenResponse;
				onSetSession(data);
				return true;
			} else {
				onClearSession();
				return false;
			}
		} catch {
			onClearSession();
			return false;
		}
	};

	const refreshSession = async (): Promise<boolean> => {
		if (inFlightRefresh) {
			return inFlightRefresh;
		}

		inFlightRefresh = (async () => {
			const locks =
				deps.locks !== undefined
					? deps.locks
					: typeof navigator !== 'undefined'
						? navigator.locks
						: null;

			if (locks?.request) {
				return await locks.request('saka-auth-refresh', async () => {
					return await executeRefresh();
				});
			}

			return await executeRefresh();
		})().finally(() => {
			inFlightRefresh = null;
		});

		return inFlightRefresh;
	};

	return {
		refreshSession
	};
}

const defaultRefresher = createRefresher();

export function refreshSession(): Promise<boolean> {
	return defaultRefresher.refreshSession();
}
