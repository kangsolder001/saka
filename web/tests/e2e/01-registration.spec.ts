import { test, expect } from '@playwright/test';
import { generateUniqueUser, clearRateLimits } from './helpers/users';
import { getLatestEmail } from './helpers/mailpit';

test.describe('Registration and Email Verification Flow', () => {
	test.beforeEach(async () => {
		await clearRateLimits();
	});

	test('Scenario 1: Registration -> verify via email link -> login -> dashboard', async ({
		page
	}) => {
		const user = generateUniqueUser('reg');

		// 1. Navigate to Registration page
		await page.goto('/register');
		await expect(page.locator('input#name')).toBeVisible({ timeout: 15000 });

		// 2. Fill registration form
		await page.locator('input#name').fill(user.name);
		await page.locator('input#email').fill(user.email);
		await page.locator('input#password').fill(user.password);
		await page.locator('input#organization_name').fill(user.orgName);

		// 3. Submit form
		await page.locator('button[type="submit"]').click();

		// 4. Expect redirect to check-email confirmation page
		await page.waitForURL((url) => url.pathname.includes('/register/check-email'), {
			timeout: 10000
		});
		await expect(page.getByText(user.email).first()).toBeVisible();

		// 5. Fetch verification email from Mailpit
		const emailMsg = await getLatestEmail(user.email);
		expect(emailMsg.subject).toMatch(/verif/i);

		const verifyLink = emailMsg.links.find((l) => l.includes('/verify-email'));
		expect(verifyLink).toBeDefined();

		// 6. Navigate to the verification link
		await page.goto(verifyLink!);

		// 7. Verify email verification success state
		await expect(
			page.getByText(/(email berhasil diverifikasi|email successfully verified)/i)
		).toBeVisible({ timeout: 10000 });

		// 8. Click login button
		const loginBtn = page.locator('a[href="/login"]');
		await expect(loginBtn).toBeVisible();
		await loginBtn.click();

		await page.waitForURL((url) => url.pathname.endsWith('/login'), { timeout: 10000 });

		// 9. Login with registered credentials
		await page.locator('input#email').fill(user.email);
		await page.locator('input#password').fill(user.password);
		await page.locator('button[type="submit"]').click();

		// 10. Expect redirection to dashboard
		await page.waitForURL((url) => url.pathname.includes('/orgs/'), { timeout: 10000 });
		await expect(page.locator('main')).toBeVisible();
	});
});
