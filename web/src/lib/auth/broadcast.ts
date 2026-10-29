import { clearSession } from '$lib/auth/session.svelte';
import { queryClient } from '$lib/api/query-client';
import { goto } from '$app/navigation';

export interface AuthBroadcastMessage {
	type: 'logout';
}

export function initAuthBroadcast(onLogout?: () => void): () => void {
	if (typeof globalThis === 'undefined' || typeof globalThis.BroadcastChannel !== 'function') {
		return () => {};
	}

	const channel = new BroadcastChannel('saka-auth');
	channel.onmessage = (event: MessageEvent<AuthBroadcastMessage>) => {
		if (event.data?.type === 'logout') {
			clearSession();
			queryClient.clear();
			if (onLogout) {
				onLogout();
			} else {
				void goto('/login');
			}
		}
	};

	return () => {
		channel.close();
	};
}
