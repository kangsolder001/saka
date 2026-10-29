import { describe, expect, it, beforeEach, vi } from 'vitest';
import { safeRedirect, ensureSession } from './guards';
import { session, clearSession, setSession } from '$lib/auth/session.svelte';
import type { components } from '$lib/api/schema';

describe('safeRedirect', () => {
	it('returns internal path unchanged', () => {
		expect(safeRedirect('/orgs')).toBe('/orgs');
		expect(safeRedirect('/orgs/123?tab=billing')).toBe('/orgs/123?tab=billing');
		expect(safeRedirect('/')).toBe('/');
	});

	it('trims whitespace around valid internal paths', () => {
		expect(safeRedirect('  /profile  ')).toBe('/profile');
	});

	it('falls back to defaultPath for empty or non-string values', () => {
		expect(safeRedirect(null)).toBe('/');
		expect(safeRedirect(undefined)).toBe('/');
		expect(safeRedirect('')).toBe('/');
		expect(safeRedirect('   ')).toBe('/');
		expect(safeRedirect('', '/fallback')).toBe('/fallback');
	});

	it('blocks external protocol URLs', () => {
		expect(safeRedirect('https://evil.com')).toBe('/');
		expect(safeRedirect('http://evil.com/orgs')).toBe('/');
		expect(safeRedirect('javascript:alert(1)')).toBe('/');
		expect(safeRedirect('data:text/html,evil')).toBe('/');
	});

	it('blocks protocol-relative URLs (starts with //)', () => {
		expect(safeRedirect('//evil.com')).toBe('/');
		expect(safeRedirect('//evil.com/path')).toBe('/');
	});

	it('blocks backslash tricks (/\\ and /\\evil)', () => {
		expect(safeRedirect('/\\evil.com')).toBe('/');
		expect(safeRedirect('/\\')).toBe('/');
	});

	it('blocks control characters', () => {
		expect(safeRedirect('/path\r\nevil')).toBe('/');
		expect(safeRedirect('/path\x00evil')).toBe('/');
	});
});

describe('ensureSession', () => {
	const sampleToken: components['schemas']['TokenResponse'] = {
		access_token: 'jwt-access-token',
		token_type: 'Bearer',
		expires_in: 900,
		user: {
			id: 'usr-1',
			email: 'user@example.com',
			name: 'Test User',
			email_verified: true
		}
	};

	beforeEach(() => {
		clearSession();
		session.status = 'unknown';
	});

	it('returns true immediately if session is already authenticated', async () => {
		setSession(sampleToken);
		const refresher = vi.fn().mockResolvedValue(true);

		const result = await ensureSession(false, refresher);
		expect(result).toBe(true);
		expect(refresher).not.toHaveBeenCalled();
	});

	it('calls refresher when session status is unknown', async () => {
		const refresher = vi.fn().mockImplementation(async () => {
			setSession(sampleToken);
			return true;
		});

		const result = await ensureSession(false, refresher);
		expect(result).toBe(true);
		expect(refresher).toHaveBeenCalledOnce();
		expect(session.status).toBe('authenticated');
	});

	it('returns false and sets anonymous when refresher returns false', async () => {
		const refresher = vi.fn().mockImplementation(async () => {
			clearSession();
			return false;
		});

		const result = await ensureSession(false, refresher);
		expect(result).toBe(false);
		expect(refresher).toHaveBeenCalledOnce();
		expect(session.status).toBe('anonymous');
	});

	it('forces refresh when force is true even if authenticated', async () => {
		setSession(sampleToken);
		const refresher = vi.fn().mockResolvedValue(true);

		const result = await ensureSession(true, refresher);
		expect(result).toBe(true);
		expect(refresher).toHaveBeenCalledOnce();
	});
});
