import { client } from '$lib/api/client';
import { clearSession } from '$lib/auth/session.svelte';
import { queryClient } from '$lib/api/query-client';
import { goto } from '$app/navigation';

export async function logout(): Promise<void> {
	try {
		await client.POST('/auth/logout');
	} catch {
		// Ignore error per §6.4
	}
	clearSession();
	queryClient.clear();
	if (typeof window !== 'undefined' && 'BroadcastChannel' in window) {
		try {
			const channel = new BroadcastChannel('saka-auth');
			channel.postMessage({ type: 'logout' });
			channel.close();
		} catch {
			// Ignore broadcast error
		}
	}
	await goto('/login');
}
