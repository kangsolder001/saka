import type { AuthUser, TokenResponse } from '$lib/api/types';

export type SessionStatus = 'unknown' | 'authenticated' | 'anonymous';

export interface SessionState {
	status: SessionStatus;
	accessToken: string | null;
	expiresAt: number | null;
	user: AuthUser | null;
}

export const session = $state<SessionState>({
	status: 'unknown',
	accessToken: null,
	expiresAt: null,
	user: null
});

export function setSession(tokenResponse: TokenResponse, now: number = Date.now()): void {
	session.status = 'authenticated';
	session.accessToken = tokenResponse.access_token;
	session.expiresAt = now + tokenResponse.expires_in * 1000;
	if (tokenResponse.user) {
		session.user = tokenResponse.user;
	}
}

export function clearSession(): void {
	session.status = 'anonymous';
	session.accessToken = null;
	session.expiresAt = null;
	session.user = null;
}

export function isExpiringSoon(now: number = Date.now(), marginMs: number = 30_000): boolean {
	if (!session.accessToken || session.expiresAt === null) {
		return true;
	}
	return session.expiresAt - now <= marginMs;
}
