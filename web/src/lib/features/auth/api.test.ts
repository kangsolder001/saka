import { describe, expect, it, vi, beforeEach } from 'vitest';
import {
	confirmEmailChange,
	forgotPassword,
	login,
	register,
	resendVerification,
	resetPassword,
	verifyEmail
} from './api';
import { client } from '$lib/api/client';
import * as sessionModule from '$lib/auth/session.svelte';
import type { TokenResponse } from '$lib/api/types';

describe('auth api', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('login calls /auth/login and sets session', async () => {
		const mockTokenRes: TokenResponse = {
			access_token: 'fake-jwt',
			token_type: 'Bearer',
			expires_in: 900,
			user: {
				id: 'u1',
				name: 'Test User',
				email: 'test@example.com',
				email_verified: true
			}
		};

		vi.spyOn(client, 'POST').mockResolvedValue({
			data: mockTokenRes,
			response: new Response(JSON.stringify(mockTokenRes), { status: 200 })
		} as never);

		const setSessionSpy = vi.spyOn(sessionModule, 'setSession');

		const result = await login({ email: 'test@example.com', password: 'password123' });
		expect(result).toEqual(mockTokenRes);
		expect(setSessionSpy).toHaveBeenCalledWith(mockTokenRes);
	});

	it('register calls /auth/register', async () => {
		const mockUser = {
			id: 'u2',
			name: 'New User',
			email: 'new@example.com',
			email_verified: false,
			avatar_url: null,
			created_at: '2026-01-01T00:00:00Z'
		};

		vi.spyOn(client, 'POST').mockResolvedValue({
			data: { user: mockUser },
			response: new Response(JSON.stringify({ user: mockUser }), { status: 201 })
		} as never);

		const result = await register({
			name: 'New User',
			email: 'new@example.com',
			password: 'password123',
			organization_name: 'Acme'
		});

		expect(result.user).toEqual(mockUser);
	});

	it('forgotPassword calls /auth/forgot-password', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 200 })
		} as never);

		await forgotPassword('test@example.com');
		expect(postSpy).toHaveBeenCalledWith(
			'/auth/forgot-password',
			expect.objectContaining({ body: { email: 'test@example.com' } })
		);
	});

	it('resetPassword calls /auth/reset-password', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 200 })
		} as never);

		await resetPassword('token-123', 'newPassword123');
		expect(postSpy).toHaveBeenCalledWith(
			'/auth/reset-password',
			expect.objectContaining({ body: { token: 'token-123', password: 'newPassword123' } })
		);
	});

	it('verifyEmail calls /auth/verify-email', async () => {
		vi.spyOn(client, 'POST').mockResolvedValue({
			data: { email_verified: true },
			response: new Response(JSON.stringify({ email_verified: true }), { status: 200 })
		} as never);

		const result = await verifyEmail('token-123');
		expect(result.email_verified).toBe(true);
	});

	it('confirmEmailChange calls /me/email/confirm', async () => {
		vi.spyOn(client, 'POST').mockResolvedValue({
			data: { email: 'new@example.com', email_verified: true },
			response: new Response(JSON.stringify({ email: 'new@example.com', email_verified: true }), {
				status: 200
			})
		} as never);

		const result = await confirmEmailChange('token-123');
		expect(result.email).toBe('new@example.com');
	});

	it('resendVerification calls /auth/resend-verification', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 200 })
		} as never);

		await resendVerification();
		expect(postSpy).toHaveBeenCalledWith('/auth/resend-verification');
	});
});
