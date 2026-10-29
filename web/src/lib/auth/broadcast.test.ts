import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { initAuthBroadcast } from './broadcast';
import * as sessionModule from '$lib/auth/session.svelte';
import { queryClient } from '$lib/api/query-client';

describe('initAuthBroadcast', () => {
	const holder: {
		channel: {
			onmessage: ((event: MessageEvent) => void) | null;
			close: ReturnType<typeof vi.fn>;
		} | null;
	} = { channel: null };

	beforeEach(() => {
		class MockBroadcastChannel {
			onmessage: ((event: MessageEvent) => void) | null = null;
			close = vi.fn();
			constructor() {
				holder.channel = this;
			}
		}

		vi.stubGlobal('BroadcastChannel', MockBroadcastChannel);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it('returns a no-op cleanup when BroadcastChannel is unavailable', () => {
		vi.stubGlobal('BroadcastChannel', undefined);
		const cleanup = initAuthBroadcast();
		expect(typeof cleanup).toBe('function');
		cleanup();
	});

	it('handles logout message by clearing session and query cache', () => {
		const clearSessionSpy = vi.spyOn(sessionModule, 'clearSession');
		const clearQuerySpy = vi.spyOn(queryClient, 'clear');
		const onLogout = vi.fn();

		const cleanup = initAuthBroadcast(onLogout);
		expect(holder.channel?.onmessage).toBeDefined();

		holder.channel!.onmessage!({
			data: { type: 'logout' }
		} as MessageEvent);

		expect(clearSessionSpy).toHaveBeenCalled();
		expect(clearQuerySpy).toHaveBeenCalled();
		expect(onLogout).toHaveBeenCalled();

		cleanup();
		expect(holder.channel?.close).toHaveBeenCalled();
	});
});
