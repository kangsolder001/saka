import { test, expect } from '@playwright/test';
import { createVerifiedUserViaApi, clearRateLimits } from './helpers/auth';
import { getLatestEmail } from './helpers/mailpit';

test.describe('Password Reset Flow', () => {
	test.beforeEach(async () => {
		await clearRateLimits();
	});

	test('Scenario 3: Forgot password -> receive email in Mailpit -> reset password -> login with new password', async ({
		page
	}) => {
		// 1. Setup verified user
		const user = await createVerifiedUserViaApi('pwreset');
		const newPassword = `NewP@ssword-${Date.now()}`;

		// 2. Go to forgot-password page
		await page.goto('/forgot-password');
		await expect(page.locator('input#email')).toBeVisible({ timeout: 15000 });

		// 3. Request password reset link
		await page.locator('input#email').fill(user.email);
		await page.locator('button[type="submit"]').click();

		// 4. Verify request submission feedback
		await expect(
			page.getByText(/(kami sudah mengirim tautan reset|we have sent a password reset link)/i)
		).toBeVisible({ timeout: 10000 });

		// 5. Query Mailpit for password reset email
		const resetEmail = await getLatestEmail(user.email, { subject: /reset/i });
		expect(resetEmail.subject).toMatch(/reset/i);

		const resetLink = resetEmail.links.find((l) => l.includes('/reset-password'));
		expect(resetLink).toBeDefined();

		// 6. Navigate to the reset password link
		await page.goto(resetLink!);

		// 7. Fill new password and confirmation
		await page.locator('input#password').fill(newPassword);
		await page.locator('input#confirmPassword').fill(newPassword);
		await page.locator('button[type="submit"]').click();

		// 8. Expect redirect back to /login
		await page.waitForURL((url) => url.pathname.endsWith('/login'), { timeout: 10000 });

		// 9. Verify old password no longer works
		await page.locator('input#email').fill(user.email);
		await page.locator('input#password').fill(user.password);
		await page.locator('button[type="submit"]').click();

		const alertBox = page.getByRole('alert');
		await expect(alertBox).toBeVisible({ timeout: 5000 });
		await expect(alertBox).toHaveText(/(email atau password salah|invalid email or password)/i);

		// 10. Login with new password works
		await page.locator('input#password').fill(newPassword);
		await page.locator('button[type="submit"]').click();

		// 11. Successfully reaches dashboard
		await page.waitForURL((url) => url.pathname.includes('/orgs/'), { timeout: 10000 });
		await expect(page.locator('main')).toBeVisible();
	});
});
