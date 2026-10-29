import { describe, it, expect, beforeEach } from 'vitest';
import { session, setSession, clearSession, isExpiringSoon } from './session.svelte';
import type { TokenResponse, AuthUser } from '$lib/api/types';

describe('session store', () => {
	const mockUser: AuthUser = {
		id: 'user-1',
		email: 'test@example.com',
		name: 'Test User',
		email_verified: true
	};

	const mockTokenResponse: TokenResponse = {
		access_token: 'mock-jwt-token',
		token_type: 'Bearer',
		expires_in: 900,
		user: mockUser
	};

	beforeEach(() => {
		clearSession();
	});

	it('initializes / clears with anonymous state', () => {
		expect(session.status).toBe('anonymous');
		expect(session.accessToken).toBeNull();
		expect(session.expiresAt).toBeNull();
		expect(session.user).toBeNull();
	});

	it('sets session correctly with token response', () => {
		const now = 1000000;
		setSession(mockTokenResponse, now);

		expect(session.status).toBe('authenticated');
		expect(session.accessToken).toBe('mock-jwt-token');
		expect(session.expiresAt).toBe(now + 900 * 1000);
		expect(session.user).toEqual(mockUser);
	});

	it('evaluates isExpiringSoon correctly', () => {
		const now = 1000000;
		// When no token
		expect(isExpiringSoon(now)).toBe(true);

		// With fresh token (900s remaining)
		setSession(mockTokenResponse, now);
		expect(isExpiringSoon(now)).toBe(false);

		// With token expiring in 29 seconds (within 30s margin)
		const nearExpiryNow = now + 900 * 1000 - 29 * 1000;
		expect(isExpiringSoon(nearExpiryNow, 30_000)).toBe(true);

		// With token expiring in 31 seconds (outside 30s margin)
		const safeNow = now + 900 * 1000 - 31 * 1000;
		expect(isExpiringSoon(safeNow, 30_000)).toBe(false);
	});

	it('clears session properly', () => {
		setSession(mockTokenResponse);
		expect(session.status).toBe('authenticated');

		clearSession();
		expect(session.status).toBe('anonymous');
		expect(session.accessToken).toBeNull();
		expect(session.expiresAt).toBeNull();
		expect(session.user).toBeNull();
	});
});
