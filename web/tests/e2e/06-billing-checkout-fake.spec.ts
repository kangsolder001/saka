import { test, expect } from '@playwright/test';
import { generateTestUser, registerAndVerifyViaApi, loginViaUI } from './helpers/users';

test.describe('Billing Checkout & Fake Payment Simulation (§11.2 Skenario 5)', () => {
	test('owner checks out Pro plan, pays via fake payment dialog, polling marks active, schedules downgrade, cancels downgrade, cancels subscription, resumes subscription', async ({
		page
	}) => {
		test.setTimeout(60000);

		// 1. Setup owner user and organization
		const owner = generateTestUser('billing-owner');
		const { orgId } = await registerAndVerifyViaApi(owner);

		// Owner logs in
		await loginViaUI(page, owner.email, owner.password);

		// 2. Owner navigates to billing plans catalog
		await page.goto(`/orgs/${orgId}/billing/plans`);
		await page.waitForLoadState('networkidle');
		await expect(page.getByTestId('plans-page')).toBeVisible();

		// 3. Chooses Pro plan
		const proCard = page.getByTestId('plan-card-pro');
		await expect(proCard).toBeVisible();
		await proCard.getByTestId('plan-action-btn').click();

		// Confirmation dialog before purchase
		await expect(page.getByTestId('plan-confirm-dialog')).toBeVisible();
		await page.getByTestId('confirm-dialog-proceed').click();

		// If trial starts, it redirects to /billing. In DEV, paying during trial or checkout
		// opens FakePaymentDialog. If redirected to /billing, return to /plans and click "Bayar sekarang".
		const fakeDialog = page.getByTestId('fake-payment-dialog');
		try {
			await expect(fakeDialog).toBeVisible({ timeout: 5000 });
		} catch {
			// Trial started, navigated to /billing. Go back to plans to pay early
			await page.goto(`/orgs/${orgId}/billing/plans`);
			await page.waitForLoadState('networkidle');
			const payNowBtn = page.getByTestId('plan-card-pro').getByTestId('plan-action-btn');
			await payNowBtn.click();
			await expect(page.getByTestId('plan-confirm-dialog')).toBeVisible();
			await page.getByTestId('confirm-dialog-proceed').click();
			await expect(fakeDialog).toBeVisible({ timeout: 10000 });
		}

		// 4. Fake Payment Dialog is open; click "Bayar sekarang (Dev)"
		await expect(page.getByTestId('fake-invoice-number')).toBeVisible();
		await page.getByTestId('fake-pay-btn').click();

		// 5. App redirects to /billing/return where polling checks invoice payment
		await page.waitForURL(/\/billing\/return/, { timeout: 15000 });
		await expect(page.getByTestId('billing-return-paid')).toBeVisible({ timeout: 15000 });

		// Click "Ke Billing" button to navigate to billing overview
		await page.getByTestId('billing-return-billing-btn').click();
		await page.waitForURL(`/orgs/${orgId}/billing`, { timeout: 10000 });

		// 6. Verify subscription is now active on Pro plan
		await expect(page.getByTestId('billing-overview-page')).toBeVisible();
		await expect(page.getByTestId('billing-plan-name')).toContainText(/pro/i);
		await expect(page.locator('[data-status="active"]')).toBeVisible();

		// 7. Upgrade to Business so we can test scheduling downgrade to Pro
		await page.goto(`/orgs/${orgId}/billing/plans`);
		await page.waitForLoadState('networkidle');
		const businessCard = page.getByTestId('plan-card-business');
		await businessCard.getByTestId('plan-action-btn').click();
		await expect(page.getByTestId('plan-confirm-dialog')).toBeVisible();
		await page.getByTestId('confirm-dialog-proceed').click();

		// Pay the upgrade invoice via Fake Payment Dialog
		await expect(fakeDialog).toBeVisible({ timeout: 10000 });
		await page.getByTestId('fake-pay-btn').click();
		await page.waitForURL(/\/billing\/return/, { timeout: 15000 });
		await expect(page.getByTestId('billing-return-paid')).toBeVisible({ timeout: 15000 });
		await page.getByTestId('billing-return-billing-btn').click();
		await page.waitForURL(`/orgs/${orgId}/billing`, { timeout: 10000 });

		// Verify subscription is now active on Business plan
		await expect(page.getByTestId('billing-plan-name')).toContainText(/business/i);

		// 8. Schedule downgrade from Business to Pro
		await page.goto(`/orgs/${orgId}/billing/plans`);
		await page.waitForLoadState('networkidle');
		const proDowngradeCard = page.getByTestId('plan-card-pro');
		await proDowngradeCard.getByTestId('plan-action-btn').click();
		await expect(page.getByTestId('plan-confirm-dialog')).toBeVisible();
		await page.getByTestId('confirm-dialog-proceed').click();

		// Downgrade warning dialog appears
		const warningDialog = page.getByTestId('downgrade-warning-dialog');
		await expect(warningDialog).toBeVisible({ timeout: 10000 });
		await page.getByTestId('downgrade-acknowledge-btn').click();

		// Check pending downgrade info on billing overview
		await page.goto(`/orgs/${orgId}/billing`);
		await expect(page.getByTestId('billing-overview-page')).toBeVisible();
		await expect(page.getByTestId('billing-pending-downgrade-info')).toBeVisible();

		// 9. Cancel pending downgrade
		await page.goto(`/orgs/${orgId}/billing/plans`);
		await expect(page.getByTestId('plans-page')).toBeVisible();
		const cancelChangeBtn = page.getByTestId('plan-card-business').getByTestId('plan-action-btn');
		await cancelChangeBtn.click();
		await expect(page.getByTestId('plan-confirm-dialog')).toBeVisible();
		await page.getByTestId('confirm-dialog-proceed').click();

		// Back to billing overview: pending downgrade info is gone
		await page.goto(`/orgs/${orgId}/billing`);
		await expect(page.getByTestId('billing-overview-page')).toBeVisible();
		await expect(page.getByTestId('billing-pending-downgrade-info')).not.toBeVisible();

		// 10. Cancel subscription (cancel_at_period_end)
		const cancelSubBtn = page.getByTestId('billing-cancel-btn');
		await expect(cancelSubBtn).toBeVisible();
		await cancelSubBtn.click();

		const confirmCancelDialog = page.locator('[role="alertdialog"]');
		await expect(confirmCancelDialog).toBeVisible();
		await confirmCancelDialog.getByRole('button', { name: /ya, batalkan|yes, cancel/i }).click();

		// Cancel button disappears, resume button appears
		await expect(page.getByTestId('billing-resume-btn')).toBeVisible({ timeout: 10000 });
		await expect(page.getByTestId('billing-cancel-btn')).not.toBeVisible();

		// 11. Resume subscription
		await page.getByTestId('billing-resume-btn').click();
		await expect(page.getByTestId('billing-cancel-btn')).toBeVisible({ timeout: 10000 });
		await expect(page.getByTestId('billing-resume-btn')).not.toBeVisible();
	});
});
