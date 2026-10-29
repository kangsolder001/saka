import { call, client } from '$lib/api/client';
import { setSession } from '$lib/auth/session.svelte';
import type { TokenResponse, AuthUser } from '$lib/api/types';

export interface LoginParams {
	email: string;
	password: string;
	token_delivery?: 'cookie' | 'body';
}

export interface RegisterParams {
	name: string;
	email: string;
	password: string;
	organization_name: string;
}

export async function login(params: LoginParams): Promise<TokenResponse> {
	const res = await call(() =>
		client.POST('/auth/login', {
			body: {
				token_delivery: 'cookie',
				...params
			}
		})
	);
	setSession(res);
	return res;
}

export async function register(params: RegisterParams): Promise<{ user: AuthUser }> {
	return call(() =>
		client.POST('/auth/register', {
			body: params
		})
	);
}

export async function forgotPassword(email: string): Promise<void> {
	await call(() =>
		client.POST('/auth/forgot-password', {
			body: { email }
		})
	);
}

export async function resetPassword(token: string, password: string): Promise<void> {
	await call(() =>
		client.POST('/auth/reset-password', {
			body: { token, password }
		})
	);
}

export async function verifyEmail(token: string): Promise<{ email_verified: true }> {
	return call(() =>
		client.POST('/auth/verify-email', {
			body: { token }
		})
	);
}

export async function confirmEmailChange(
	token: string
): Promise<{ email: string; email_verified: boolean }> {
	return call(() =>
		client.POST('/me/email/confirm', {
			body: { token }
		})
	);
}

export async function resendVerification(): Promise<void> {
	await call(() => client.POST('/auth/resend-verification'));
}
