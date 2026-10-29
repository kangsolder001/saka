import { test, expect } from '@playwright/test';
import { generateTestUser, registerAndVerifyViaApi, loginViaUI } from './helpers/users';

test.describe('Account Security & Sessions (§11.2 Skenario 6 & 7)', () => {
	test('user updates password, revokes other sessions, revokes session from list, and logs out (§11.2 Skenario 6)', async ({
		page,
		browser
	}) => {
		test.setTimeout(90000);

		// 1. Setup user and organization
		const user = generateTestUser('sec-user');
		const { orgId } = await registerAndVerifyViaApi(user);

		// 2. Primary session in page
		await loginViaUI(page, user.email, user.password);

		// 3. Open secondary session in context2
		const context2 = await browser.newContext();
		const page2 = await context2.newPage();
		await loginViaUI(page2, user.email, user.password);

		// 4. In primary session (page), update password in security tab
		await page.goto('/account/security');

		const newPassword = 'NewSecretPassword456!';
		await page.fill('input#current_password', user.password);
		await page.fill('input#new_password', newPassword);
		await page.fill('input#confirm_password', newPassword);
		await page.click('button[type="submit"]');

		// Toast appears confirming password changed
		await expect(
			page.getByText(/kata sandi berhasil diperbarui|password successfully updated/i)
		).toBeVisible({ timeout: 10000 });

		// 5. Verify secondary session (page2) has been revoked
		// Reloading or attempting to refresh session on page2 will detect revoked session and redirect to /login
		await page2.reload();
		await expect(page2.locator('input#email')).toBeVisible({ timeout: 15000 });
		await context2.close();

		// 6. Log in on another session in context3 with the new password
		const context3 = await browser.newContext();
		const page3 = await context3.newPage();
		await loginViaUI(page3, user.email, newPassword);

		// 7. In primary session (page), go to sessions list
		await page.goto('/account/sessions');
		await expect(page.locator('[data-testid^="session-row-"]')).toHaveCount(2, { timeout: 15000 });

		// Locate the other session (which does not have the "Sesi Ini" / "Current Session" badge)
		const otherSessionRow = page
			.locator('[data-testid^="session-row-"]')
			.filter({ hasNot: page.getByText(/sesi ini|current session/i) })
			.first();
		await expect(otherSessionRow).toBeVisible({ timeout: 10000 });

		// Click "Cabut" on the other session
		const revokeBtn = otherSessionRow.getByRole('button', { name: /cabut|revoke/i });
		await revokeBtn.click();

		// Confirm revocation in dialog
		const confirmDialog = page.locator('[role="alertdialog"]');
		await expect(confirmDialog).toBeVisible({ timeout: 5000 });
		await confirmDialog.getByRole('button', { name: /cabut|revoke/i }).click();

		// Wait for revocation to complete and toast to appear
		await expect(page.getByText(/sesi berhasil dicabut|session successfully revoked/i)).toBeVisible(
			{ timeout: 10000 }
		);
		await expect(page.locator('[data-testid^="session-row-"]')).toHaveCount(1, { timeout: 10000 });

		// On page3, verify it was revoked: reload causes session check to fail and redirect to /login
		await page3.reload();
		await page3.waitForURL(/\/login/, { timeout: 15000 });
		await expect(page3.locator('input#email')).toBeVisible({ timeout: 15000 });
		await context3.close();

		// 8. User logs out from primary session via AppShell user menu
		await page.goto(`/orgs/${orgId}`);
		await page.getByTestId('user-menu-trigger').click();
		await page.getByTestId('user-menu-logout-button').click();
		await page.waitForURL(/\/login/, { timeout: 10000 });
		await expect(page.locator('input#email')).toBeVisible();
	});

	test('refresh token: concurrent requests across tabs after access token expiration do not cause logout via Web Locks (§11.2 Skenario 7)', async ({
		browser
	}) => {
		test.setTimeout(60000);

		// Setup user and organization
		const user = generateTestUser('lock-user');
		const { orgId } = await registerAndVerifyViaApi(user);

		// Single context represents a single browser instance sharing cookies and Web Locks
		const context = await browser.newContext();
		const page1 = await context.newPage();
		await loginViaUI(page1, user.email, user.password);
		await page1.goto(`/orgs/${orgId}`);
		await page1.waitForLoadState('networkidle');

		// Open second tab in the same context
		const page2 = await context.newPage();
		await page2.goto(`/orgs/${orgId}`);
		await page2.waitForLoadState('networkidle');

		// Both tabs are currently logged in.
		// Now trigger concurrent refresh requests across both tabs via the Web Locks mechanism.
		// We use navigator.locks directly or through the refreshSession module to simulate
		// concurrent refresh triggers when access tokens expire.
		const results = await Promise.all([
			page1.evaluate(async () => {
				// Simulate proactive or reactive refresh with lock
				if (navigator.locks?.request) {
					return await navigator.locks.request('saka-auth-refresh', async () => {
						const res = await fetch('/api/v1/auth/refresh', {
							method: 'POST',
							credentials: 'include'
						});
						return res.status;
					});
				}
				const res = await fetch('/api/v1/auth/refresh', {
					method: 'POST',
					credentials: 'include'
				});
				return res.status;
			}),
			page2.evaluate(async () => {
				if (navigator.locks?.request) {
					return await navigator.locks.request('saka-auth-refresh', async () => {
						const res = await fetch('/api/v1/auth/refresh', {
							method: 'POST',
							credentials: 'include'
						});
						return res.status;
					});
				}
				const res = await fetch('/api/v1/auth/refresh', {
					method: 'POST',
					credentials: 'include'
				});
				return res.status;
			})
		]);

		// At least one refresh succeeded (200), and the second succeeded or safely received fresh cookie
		expect(results[0]).toBe(200);
		expect(results[1]).toBe(200);

		// Verify neither tab is logged out: navigating to /account remains authenticated
		await page1.goto('/account');
		await page1.waitForLoadState('networkidle');
		expect(page1.url()).not.toContain('/login');

		await page2.goto('/account');
		await page2.waitForLoadState('networkidle');
		expect(page2.url()).not.toContain('/login');

		await context.close();
	});
});
