import { describe, it, expect, vi } from 'vitest';
import { createRefresher } from './refresh';
import type { TokenResponse } from '$lib/api/types';

describe('createRefresher', () => {
	const mockTokenResponse: TokenResponse = {
		access_token: 'new-access-token',
		token_type: 'Bearer',
		expires_in: 900,
		user: {
			id: 'user-1',
			email: 'user@example.com',
			name: 'Test User',
			email_verified: true
		}
	};

	const createMockResponse = (data: unknown, status = 200) => {
		return new Response(JSON.stringify(data), {
			status,
			headers: { 'Content-Type': 'application/json' }
		});
	};

	it('refreshes session on 200 OK and calls onSetSession', async () => {
		const onSetSession = vi.fn();
		const onClearSession = vi.fn();
		const fetchFn = vi
			.fn()
			.mockImplementation(() => Promise.resolve(createMockResponse(mockTokenResponse)));

		const refresher = createRefresher({
			fetchFn: fetchFn as unknown as typeof fetch,
			onSetSession,
			onClearSession,
			locks: null,
			apiBase: '/api/v1'
		});

		const result = await refresher.refreshSession();

		expect(result).toBe(true);
		expect(fetchFn).toHaveBeenCalledTimes(1);
		expect(fetchFn).toHaveBeenCalledWith('/api/v1/auth/refresh', {
			method: 'POST',
			credentials: 'include'
		});
		expect(onSetSession).toHaveBeenCalledWith(mockTokenResponse);
		expect(onClearSession).not.toHaveBeenCalled();
	});

	it('clears session and returns false on 401', async () => {
		const onSetSession = vi.fn();
		const onClearSession = vi.fn();
		const fetchFn = vi
			.fn()
			.mockImplementation(() =>
				Promise.resolve(
					createMockResponse(
						{ error: { code: 'UNAUTHORIZED', message: 'Invalid refresh token' } },
						401
					)
				)
			);

		const refresher = createRefresher({
			fetchFn: fetchFn as unknown as typeof fetch,
			onSetSession,
			onClearSession,
			locks: null,
			apiBase: '/api/v1'
		});

		const result = await refresher.refreshSession();

		expect(result).toBe(false);
		expect(fetchFn).toHaveBeenCalledTimes(1);
		expect(onSetSession).not.toHaveBeenCalled();
		expect(onClearSession).toHaveBeenCalledTimes(1);
	});

	it('clears session and returns false on network error', async () => {
		const onSetSession = vi.fn();
		const onClearSession = vi.fn();
		const fetchFn = vi.fn().mockRejectedValue(new Error('Network failure'));

		const refresher = createRefresher({
			fetchFn: fetchFn as unknown as typeof fetch,
			onSetSession,
			onClearSession,
			locks: null,
			apiBase: '/api/v1'
		});

		const result = await refresher.refreshSession();

		expect(result).toBe(false);
		expect(onSetSession).not.toHaveBeenCalled();
		expect(onClearSession).toHaveBeenCalledTimes(1);
	});

	it('merges concurrent calls into a single fetch (single-flight)', async () => {
		const onSetSession = vi.fn();
		let resolveFetch!: (res: Response) => void;
		const fetchPromise = new Promise<Response>((resolve) => {
			resolveFetch = resolve;
		});
		const fetchFn = vi.fn().mockReturnValue(fetchPromise);

		const refresher = createRefresher({
			fetchFn: fetchFn as unknown as typeof fetch,
			onSetSession,
			locks: null,
			apiBase: '/api/v1'
		});

		const p1 = refresher.refreshSession();
		const p2 = refresher.refreshSession();

		expect(fetchFn).toHaveBeenCalledTimes(1);

		resolveFetch(createMockResponse(mockTokenResponse));

		const [res1, res2] = await Promise.all([p1, p2]);

		expect(res1).toBe(true);
		expect(res2).toBe(true);
		expect(fetchFn).toHaveBeenCalledTimes(1);
		expect(onSetSession).toHaveBeenCalledTimes(1);
	});

	it('makes a new fetch after previous in-flight refresh completes', async () => {
		const onSetSession = vi.fn();
		const fetchFn = vi
			.fn()
			.mockImplementation(() => Promise.resolve(createMockResponse(mockTokenResponse)));

		const refresher = createRefresher({
			fetchFn: fetchFn as unknown as typeof fetch,
			onSetSession,
			locks: null,
			apiBase: '/api/v1'
		});

		const res1 = await refresher.refreshSession();
		expect(res1).toBe(true);
		expect(fetchFn).toHaveBeenCalledTimes(1);

		const res2 = await refresher.refreshSession();
		expect(res2).toBe(true);
		expect(fetchFn).toHaveBeenCalledTimes(2);
	});

	it('uses Web Locks API when available', async () => {
		const onSetSession = vi.fn();
		const fetchFn = vi
			.fn()
			.mockImplementation(() => Promise.resolve(createMockResponse(mockTokenResponse)));

		const mockLocks = {
			request: vi.fn(async (name: string, callback: () => Promise<unknown>) => {
				return await callback();
			})
		} as unknown as LockManager;

		const refresher = createRefresher({
			fetchFn: fetchFn as unknown as typeof fetch,
			onSetSession,
			locks: mockLocks,
			apiBase: '/api/v1'
		});

		const result = await refresher.refreshSession();

		expect(result).toBe(true);
		expect(mockLocks.request).toHaveBeenCalledTimes(1);
		expect(mockLocks.request).toHaveBeenCalledWith('saka-auth-refresh', expect.any(Function));
		expect(fetchFn).toHaveBeenCalledTimes(1);
	});
});
