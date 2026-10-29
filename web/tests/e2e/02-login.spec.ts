import { test, expect } from '@playwright/test';
import { createVerifiedUserViaApi, clearRateLimits } from './helpers/auth';

test.describe('Login Scenarios', () => {
	test.beforeEach(async () => {
		await clearRateLimits();
	});

	test('Scenario 2: Login wrong password shows error message; correct login redirects to dashboard/lastOrgId', async ({
		page
	}) => {
		// 1. Create a verified user via API
		const user = await createVerifiedUserViaApi('login');

		// 2. Go to login page
		await page.goto('/login');
		await expect(page.locator('input#email')).toBeVisible({ timeout: 15000 });

		// 3. Attempt login with incorrect password
		await page.locator('input#email').fill(user.email);
		await page.locator('input#password').fill('WrongPassword999!');
		await page.locator('button[type="submit"]').click();

		// 4. Expect error alert
		const alertBox = page.getByRole('alert');
		await expect(alertBox).toBeVisible({ timeout: 5000 });
		await expect(alertBox).toHaveText(/(email atau password salah|invalid email or password)/i);
		expect(page.url()).toContain('/login');

		// 5. Attempt login with correct password
		await page.locator('input#password').fill(user.password);
		await page.locator('button[type="submit"]').click();

		// 6. Expect successful redirection to organization dashboard
		await page.waitForURL((url) => url.pathname.includes('/orgs/'), { timeout: 10000 });
		const orgUrl = page.url();
		const orgIdMatch = orgUrl.match(/\/orgs\/([^/?]+)/);
		expect(orgIdMatch).not.toBeNull();
		const orgId = orgIdMatch![1];

		// 7. Test lastOrgId persistence: visiting "/" redirects to "/orgs/<orgId>"
		await page.goto('/');
		await page.waitForURL((url) => url.pathname === `/orgs/${orgId}`, { timeout: 10000 });
		expect(page.url()).toContain(`/orgs/${orgId}`);
	});
});
