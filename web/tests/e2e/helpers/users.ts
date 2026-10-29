import net from 'node:net';
import type { Page } from '@playwright/test';
import { getLatestEmailLink } from './mailpit';

const API_BASE = process.env.API_BASE_URL || 'http://localhost:8080/api/v1';

export interface TestUser {
	name: string;
	email: string;
	password: string;
	orgName: string;
	organizationName: string;
}

/**
 * Clears Redis cache and rate limits so tests are not blocked by 429s.
 */
export async function clearRateLimits(): Promise<void> {
	return new Promise((resolve) => {
		const socket = net.createConnection({ port: 6380, host: 'localhost' }, () => {
			socket.write('FLUSHDB\r\n');
		});
		socket.on('data', () => {
			socket.end();
			resolve();
		});
		socket.on('error', () => {
			resolve();
		});
		setTimeout(resolve, 500);
	});
}

export function createUniqueEmail(prefix = 'user'): string {
	const timestamp = Date.now();
	const random = Math.random().toString(36).slice(2, 7);
	return `${prefix}-${timestamp}-${random}@example.com`;
}

/**
 * Generates a unique test user with random credentials.
 * Password meets standard requirements (min 8 characters).
 */
export function generateUniqueUser(prefix = 'test'): TestUser {
	const timestamp = Date.now();
	const random = Math.random().toString(36).slice(2, 7);
	const email = `${prefix}-${timestamp}-${random}@example.com`;
	const orgName = `Org ${random.toUpperCase()}`;
	return {
		name: `${prefix.charAt(0).toUpperCase() + prefix.slice(1)} ${random}`,
		email,
		password: `P@ssword123-${random}`,
		orgName,
		organizationName: orgName
	};
}

export function generateTestUser(prefix = 'user'): TestUser {
	return generateUniqueUser(prefix);
}

/**
 * Registers and verifies a test user via direct backend API calls,
 * intercepting the verification link from Mailpit.
 * Returns the user, tokens, and default organization ID.
 */
export async function registerAndVerifyViaApi(user: TestUser): Promise<{
	user: TestUser;
	orgId: string;
	accessToken: string;
}> {
	// 1. Register with automatic retry if rate limited
	let regRes = await fetch(`${API_BASE}/auth/register`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			name: user.name,
			email: user.email,
			password: user.password,
			organization_name: user.organizationName || user.orgName
		})
	});

	if (regRes.status === 429) {
		await clearRateLimits();
		regRes = await fetch(`${API_BASE}/auth/register`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				name: user.name,
				email: user.email,
				password: user.password,
				organization_name: user.organizationName || user.orgName
			})
		});
	}

	if (!regRes.ok) {
		const text = await regRes.text();
		throw new Error(`Register failed with status ${regRes.status}: ${text}`);
	}

	// 2. Wait for verification email and extract token
	const verifyUrlStr = await getLatestEmailLink(user.email, 'verify-email');
	const verifyUrl = new URL(verifyUrlStr);
	const token = verifyUrl.searchParams.get('token');
	if (!token) {
		throw new Error(`Could not find token in verification URL: ${verifyUrlStr}`);
	}

	// 3. Call verify email
	const verifyRes = await fetch(`${API_BASE}/auth/verify-email`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ token })
	});

	if (!verifyRes.ok) {
		const text = await verifyRes.text();
		throw new Error(`Verify email failed with status ${verifyRes.status}: ${text}`);
	}

	// 4. Log in to get accessToken and orgId
	const loginRes = await fetch(`${API_BASE}/auth/login`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			email: user.email,
			password: user.password
		})
	});

	if (!loginRes.ok) {
		const text = await loginRes.text();
		throw new Error(`Login failed with status ${loginRes.status}: ${text}`);
	}

	const loginData = (await loginRes.json()) as {
		access_token: string;
		user: {
			id: string;
			email: string;
			organizations?: Array<{ id: string; name: string; role: string }>;
		};
	};

	let orgId = loginData.user.organizations?.[0]?.id;

	if (!orgId) {
		// fetch /me
		const meRes = await fetch(`${API_BASE}/me`, {
			headers: { Authorization: `Bearer ${loginData.access_token}` }
		});
		if (meRes.ok) {
			const meData = (await meRes.json()) as {
				organizations?: Array<{ id: string; name: string }>;
			};
			orgId = meData.organizations?.[0]?.id;
		}
	}

	if (!orgId) {
		throw new Error(`Could not determine organization ID for registered user ${user.email}`);
	}

	return {
		user,
		orgId,
		accessToken: loginData.access_token
	};
}

/**
 * Creates and verifies a user directly via backend API for fast test prerequisites.
 */
export async function createVerifiedUserViaApi(prefix = 'user'): Promise<TestUser> {
	const user = generateUniqueUser(prefix);
	const res = await registerAndVerifyViaApi(user);
	return res.user;
}

/**
 * Logs in through the browser UI and waits for navigation into the application.
 */
export async function loginViaUI(page: Page, email: string, password: string): Promise<void> {
	await page.goto('/login');
	await page.locator('input#email').fill(email);
	await page.locator('input#password').fill(password);
	await page.locator('button[type="submit"]').click();
	// Wait until redirect away from login page
	await page.waitForURL((url) => !url.pathname.endsWith('/login'), { timeout: 15000 });
}

export async function loginViaUi(
	page: Page,
	credentials: { email: string; password: string }
): Promise<void> {
	return loginViaUI(page, credentials.email, credentials.password);
}
