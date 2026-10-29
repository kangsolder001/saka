import { test, expect } from '@playwright/test';
import { createVerifiedUserViaApi, loginViaUi, clearRateLimits } from './helpers/auth';
import { getLatestEmail } from './helpers/mailpit';

test.describe('Account Email Change Flow', () => {
	test.beforeEach(async () => {
		await clearRateLimits();
	});

	test('Scenario 4: Change email in account security -> confirm via email link -> email is updated', async ({
		page
	}) => {
		// 1. Setup user and sign in
		const user = await createVerifiedUserViaApi('chgemail');
		await loginViaUi(page, user);

		// 2. Go to account security page
		await page.goto('/account/security');
		await expect(page.locator('input#new_email')).toBeVisible({ timeout: 15000 });

		const newEmail = `updated-${Date.now()}-${Math.random().toString(36).slice(2, 7)}@example.com`;

		// 3. Fill change email form
		const changeEmailForm = page.locator('form').filter({ has: page.locator('input#new_email') });
		await changeEmailForm.locator('input#new_email').fill(newEmail);
		await changeEmailForm.locator('input#email_current_password').fill(user.password);
		await changeEmailForm.locator('button[type="submit"]').click();

		// 4. Expect confirmation notification / toast
		await expect(
			page.getByText(/(tautan konfirmasi telah dikirim|confirmation link sent)/i)
		).toBeVisible({ timeout: 10000 });

		// 5. Query Mailpit for email to the NEW email address
		const confirmationEmail = await getLatestEmail(newEmail);
		const confirmLink = confirmationEmail.links.find((l) => l.includes('/confirm-email-change'));
		expect(confirmLink).toBeDefined();

		// 6. Navigate to confirmation link
		await page.goto(confirmLink!);

		// 7. Verify email confirmation success state
		await expect(page.getByText(/(dikonfirmasi|confirmed)/i)).toBeVisible({ timeout: 10000 });

		// 8. Sign in with the new email to verify credentials work end-to-end
		await page.goto('/login');
		await page.locator('input#email').fill(newEmail);
		await page.locator('input#password').fill(user.password);
		await page.locator('button[type="submit"]').click();
		await page.waitForURL((url) => url.pathname.includes('/orgs/'), { timeout: 10000 });

		// 9. Go to account profile page to verify the updated email is reflected in the UI
		await page.goto('/account');
		await expect(page.locator('input#email')).toHaveValue(newEmail, { timeout: 10000 });
	});
});
