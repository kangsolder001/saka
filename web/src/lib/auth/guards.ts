import { session } from '$lib/auth/session.svelte';
import { refreshSession } from '$lib/auth/refresh';

/**
 * Validate that a redirect path is internal only (§11.5).
 * Only paths starting with '/' and not followed by '/' or '\' are allowed.
 */
export function safeRedirect(value: string | null | undefined, defaultPath = '/'): string {
	if (!value || typeof value !== 'string') {
		return defaultPath;
	}
	const trimmed = value.trim();
	if (!trimmed || !trimmed.startsWith('/')) {
		return defaultPath;
	}
	if (trimmed.length > 1 && (trimmed[1] === '/' || trimmed[1] === '\\')) {
		return defaultPath;
	}
	for (let i = 0; i < trimmed.length; i++) {
		const code = trimmed.charCodeAt(i);
		if (code <= 31 || code === 127) {
			return defaultPath;
		}
	}
	return trimmed;
}

/**
 * Ensure the application has a valid session (§6.2).
 * When session is 'unknown', attempts to refresh.
 * Returns true if authenticated, false otherwise.
 */
export async function ensureSession(
	force = false,
	refresher: () => Promise<boolean> = refreshSession
): Promise<boolean> {
	if (!force && session.status === 'authenticated' && session.accessToken) {
		return true;
	}
	if (session.status === 'unknown' || force) {
		return refresher();
	}
	return false;
}
