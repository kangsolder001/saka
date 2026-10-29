import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
	createApiClient,
	call,
	isPublicPath,
	setOnSessionEnded,
	getOnSessionEnded
} from './client';
import { ApiError } from './errors';
import type { SessionStatus } from '$lib/auth/session.svelte';

describe('API Client', () => {
	beforeEach(() => {
		setOnSessionEnded(null);
	});

	describe('isPublicPath', () => {
		it('identifies public auth paths', () => {
			expect(isPublicPath('/auth/login', 'POST')).toBe(true);
			expect(isPublicPath('/auth/refresh', 'POST')).toBe(true);
			expect(isPublicPath('/auth/register', 'POST')).toBe(true);
			expect(isPublicPath('/auth/verify-email', 'POST')).toBe(true);
			expect(isPublicPath('/auth/forgot-password', 'POST')).toBe(true);
			expect(isPublicPath('/auth/reset-password', 'POST')).toBe(true);
			expect(isPublicPath('/me/email/confirm', 'POST')).toBe(true);
		});

		it('identifies public GET plans', () => {
			expect(isPublicPath('/plans', 'GET')).toBe(true);
			expect(isPublicPath('/plans', 'POST')).toBe(false);
		});

		it('identifies public invitation GET and decline', () => {
			expect(isPublicPath('/invitations/{token}', 'GET')).toBe(true);
			expect(isPublicPath('/invitations/{token}', 'POST')).toBe(false);
			expect(isPublicPath('/invitations/{token}/decline', 'POST')).toBe(true);
			expect(isPublicPath('/invitations/{token}/accept', 'POST')).toBe(false);
		});

		it('identifies protected endpoints', () => {
			expect(isPublicPath('/auth/me', 'GET')).toBe(false);
			expect(isPublicPath('/orgs', 'GET')).toBe(false);
			expect(isPublicPath('/orgs', 'POST')).toBe(false);
		});
	});

	describe('createApiClient middleware', () => {
		it('attaches Bearer token to protected endpoints when session has accessToken', async () => {
			let capturedAuthHeader: string | null = null;

			const mockFetch = vi.fn().mockImplementation((req: Request) => {
				capturedAuthHeader = req.headers.get('Authorization');
				return Promise.resolve(
					new Response(JSON.stringify({ id: 'user-1', name: 'Test' }), {
						status: 200,
						headers: { 'Content-Type': 'application/json' }
					})
				);
			});

			const client = createApiClient({
				apiBase: 'http://localhost:3000/api/v1',
				fetchFn: mockFetch as unknown as typeof fetch,
				sessionStore: {
					status: 'authenticated' as SessionStatus,
					accessToken: 'secret-token-123',
					expiresAt: Date.now() + 600_000,
					user: null
				},
				isExpiringSoonFn: () => false
			});

			await client.GET('/me');

			expect(mockFetch).toHaveBeenCalledTimes(1);
			expect(capturedAuthHeader).toBe('Bearer secret-token-123');
		});

		it('does NOT attach Bearer token to public endpoints', async () => {
			let capturedAuthHeader: string | null = null;

			const mockFetch = vi.fn().mockImplementation((req: Request) => {
				capturedAuthHeader = req.headers.get('Authorization');
				return Promise.resolve(
					new Response(JSON.stringify({ access_token: 'new-token' }), {
						status: 200,
						headers: { 'Content-Type': 'application/json' }
					})
				);
			});

			const client = createApiClient({
				apiBase: 'http://localhost:3000/api/v1',
				fetchFn: mockFetch as unknown as typeof fetch,
				sessionStore: {
					status: 'authenticated' as SessionStatus,
					accessToken: 'secret-token-123',
					expiresAt: Date.now() + 600_000,
					user: null
				},
				isExpiringSoonFn: () => false
			});

			await client.POST('/auth/login', {
				body: { email: 'test@example.com', password: 'password123', token_delivery: 'cookie' }
			});

			expect(mockFetch).toHaveBeenCalledTimes(1);
			expect(capturedAuthHeader).toBeNull();
		});

		it('triggers proactive refresh when isExpiringSoon is true', async () => {
			const mockRefresh = vi.fn().mockResolvedValue(true);
			const mockSession = {
				status: 'authenticated' as SessionStatus,
				accessToken: 'new-refreshed-token',
				expiresAt: Date.now() + 600_000,
				user: null
			};

			let capturedAuthHeader: string | null = null;
			const mockFetch = vi.fn().mockImplementation((req: Request) => {
				capturedAuthHeader = req.headers.get('Authorization');
				return Promise.resolve(
					new Response(JSON.stringify({ id: 'user-1' }), {
						status: 200,
						headers: { 'Content-Type': 'application/json' }
					})
				);
			});

			const client = createApiClient({
				apiBase: 'http://localhost:3000/api/v1',
				fetchFn: mockFetch as unknown as typeof fetch,
				sessionStore: mockSession,
				isExpiringSoonFn: () => true,
				refreshFn: mockRefresh
			});

			await client.GET('/me');

			expect(mockRefresh).toHaveBeenCalledTimes(1);
			expect(capturedAuthHeader).toBe('Bearer new-refreshed-token');
		});
	});

	describe('call helper', () => {
		it('returns typed data on successful call', async () => {
			const operation = vi.fn().mockResolvedValue({
				data: { id: 'user-1', name: 'Alice' },
				error: undefined,
				response: new Response(null, { status: 200 })
			});

			const result = await call(operation);

			expect(result).toEqual({ id: 'user-1', name: 'Alice' });
			expect(operation).toHaveBeenCalledTimes(1);
		});

		it('retries once on 401 TOKEN_EXPIRED and returns data if retry succeeds', async () => {
			const mockRefresh = vi.fn().mockResolvedValue(true);
			const onSessionEnded = vi.fn();

			let attempts = 0;
			const operation = vi.fn().mockImplementation(() => {
				attempts++;
				if (attempts === 1) {
					return Promise.resolve({
						data: undefined,
						error: {
							error: {
								code: 'TOKEN_EXPIRED',
								message: 'Access token expired'
							}
						},
						response: new Response(null, { status: 401 })
					});
				}
				return Promise.resolve({
					data: { id: 'user-1', name: 'Alice' },
					error: undefined,
					response: new Response(null, { status: 200 })
				});
			});

			const result = await call(operation, {
				refreshSession: mockRefresh,
				onSessionEnded
			});

			expect(result).toEqual({ id: 'user-1', name: 'Alice' });
			expect(operation).toHaveBeenCalledTimes(2);
			expect(mockRefresh).toHaveBeenCalledTimes(1);
			expect(onSessionEnded).not.toHaveBeenCalled();
		});

		it('throws ApiError and calls onSessionEnded when retry fails after TOKEN_EXPIRED (fails twice)', async () => {
			const mockRefresh = vi.fn().mockResolvedValue(true);
			const onSessionEnded = vi.fn();

			const operation = vi.fn().mockResolvedValue({
				data: undefined,
				error: {
					error: {
						code: 'TOKEN_EXPIRED',
						message: 'Token still expired'
					}
				},
				response: new Response(null, { status: 401 })
			});

			await expect(
				call(operation, {
					refreshSession: mockRefresh,
					onSessionEnded
				})
			).rejects.toThrow(ApiError);

			expect(operation).toHaveBeenCalledTimes(2);
			expect(mockRefresh).toHaveBeenCalledTimes(1);
			expect(onSessionEnded).toHaveBeenCalledTimes(1);
		});

		it('throws ApiError and calls onSessionEnded when refreshSession returns false', async () => {
			const mockRefresh = vi.fn().mockResolvedValue(false);
			const onSessionEnded = vi.fn();

			const operation = vi.fn().mockResolvedValue({
				data: undefined,
				error: {
					error: {
						code: 'TOKEN_EXPIRED',
						message: 'Token expired'
					}
				},
				response: new Response(null, { status: 401 })
			});

			await expect(
				call(operation, {
					refreshSession: mockRefresh,
					onSessionEnded
				})
			).rejects.toThrow(ApiError);

			expect(operation).toHaveBeenCalledTimes(1);
			expect(mockRefresh).toHaveBeenCalledTimes(1);
			expect(onSessionEnded).toHaveBeenCalledTimes(1);
		});

		it('throws ApiError without calling onSessionEnded on non-auth error', async () => {
			const onSessionEnded = vi.fn();

			const operation = vi.fn().mockResolvedValue({
				data: undefined,
				error: {
					error: {
						code: 'NOT_FOUND',
						message: 'Resource not found'
					}
				},
				response: new Response(null, { status: 404 })
			});

			await expect(
				call(operation, {
					onSessionEnded
				})
			).rejects.toThrow(ApiError);

			expect(operation).toHaveBeenCalledTimes(1);
			expect(onSessionEnded).not.toHaveBeenCalled();
		});

		it('uses global onSessionEnded when set via setOnSessionEnded', async () => {
			const globalEnded = vi.fn();
			setOnSessionEnded(globalEnded);
			expect(getOnSessionEnded()).toBe(globalEnded);

			const operation = vi.fn().mockResolvedValue({
				data: undefined,
				error: {
					error: {
						code: 'UNAUTHENTICATED',
						message: 'No auth credentials'
					}
				},
				response: new Response(null, { status: 401 })
			});

			await expect(call(operation)).rejects.toThrow(ApiError);

			expect(globalEnded).toHaveBeenCalledTimes(1);
		});
	});
});
