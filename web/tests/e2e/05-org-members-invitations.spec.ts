import { test, expect } from '@playwright/test';
import {
	generateTestUser,
	createUniqueEmail,
	registerAndVerifyViaApi,
	loginViaUI
} from './helpers/users';
import { getLatestEmailLink } from './helpers/mailpit';

test.describe('Org Members and Invitations (§11.2 Skenario 3 & 4)', () => {
	test('owner invites, invitee registers/accepts, role changed, member removed, and free limit triggers upgrade dialog', async ({
		page,
		browser
	}) => {
		// 1. Setup owner user and organization
		const owner = generateTestUser('owner');
		const { orgId } = await registerAndVerifyViaApi(owner);

		// Owner logs in
		await loginViaUI(page, owner.email, owner.password);
		await page.goto(`/orgs/${orgId}/members`);
		await page.waitForLoadState('networkidle');

		// Verify member list shows owner
		await expect(page.getByTestId('members-page')).toBeVisible();
		await expect(page.getByTestId('self-badge')).toBeVisible();
		await expect(page.locator('table').getByText(owner.email)).toBeVisible();

		// 2. Owner invites a new email address
		const inviteeEmail = createUniqueEmail('invitee');
		await page.getByTestId('invite-member-btn').click();
		await expect(page.getByTestId('invite-dialog')).toBeVisible();

		await page.getByTestId('invite-email-input').fill(inviteeEmail);
		await page.getByTestId('invite-submit-btn').click();

		// Invite dialog should close upon success
		await expect(page.getByTestId('invite-dialog')).not.toBeVisible({ timeout: 10000 });

		// 3. Invitee receives invitation email in Mailpit
		const inviteLink = await getLatestEmailLink(inviteeEmail, 'accept-invitation');
		expect(inviteLink).toBeTruthy();

		// Invitee opens invitation link in a fresh browser context
		const inviteeContext = await browser.newContext();
		const inviteePage = await inviteeContext.newPage();
		await inviteePage.goto(inviteLink);
		await inviteePage.waitForLoadState('networkidle');

		// Invitee sees invitation page and clicks register to accept
		const registerLink = inviteePage.locator('a[href*="/register?invitation="]');
		await expect(registerLink).toBeVisible();
		await registerLink.click();
		await inviteePage.waitForURL(/\/register/);

		// Complete registration
		await inviteePage.fill('input#name', 'Invited Member');
		await inviteePage.fill('input#password', 'Password123!');
		await inviteePage.click('button[type="submit"]');

		// Wait for check-email page
		await inviteePage.waitForURL(/\/register\/check-email/);

		// Invitee receives verification email
		const verifyLink = await getLatestEmailLink(inviteeEmail, 'verify-email');
		await inviteePage.goto(verifyLink);
		await inviteePage.waitForLoadState('networkidle');

		// Verification is processed, log in as invitee
		await inviteePage.goto('/login');
		await inviteePage.fill('input#email', inviteeEmail);
		await inviteePage.fill('input#password', 'Password123!');
		await inviteePage.click('button[type="submit"]');

		// Invitee is now logged in and redirected into the organization
		await inviteePage.waitForURL((url) => url.pathname.includes(`/orgs/`), { timeout: 15000 });
		await inviteeContext.close();

		// 4. Back on owner's page: new member appears in members list
		await page.reload();
		await page.waitForLoadState('networkidle');
		const inviteeRow = page.locator('tr').filter({ hasText: inviteeEmail });
		await expect(inviteeRow).toBeVisible({ timeout: 10000 });

		// 5. Owner changes role from member to admin
		const roleSelectTrigger = inviteeRow.locator('button[data-testid^="role-select-trigger-"]');
		await expect(roleSelectTrigger).toBeVisible();
		await roleSelectTrigger.click();

		// Select admin option
		const adminOption = page.getByRole('option', { name: /admin/i });
		await expect(adminOption).toBeVisible();
		await adminOption.click();

		// Wait for role change to take effect
		await expect(inviteeRow.locator('button[data-testid^="role-select-trigger-"]')).toHaveText(
			/admin/i,
			{ timeout: 5000 }
		);

		// 6. Owner removes member
		const removeBtn = inviteeRow.locator('button[data-testid^="remove-member-btn-"]');
		await expect(removeBtn).toBeVisible();
		await removeBtn.click();

		// Confirm dialog opens
		const alertDialog = page.locator('[role="alertdialog"]');
		await expect(alertDialog).toBeVisible();
		const confirmRemoveBtn = alertDialog.getByRole('button', { name: /keluarkan|remove/i });
		await confirmRemoveBtn.click();

		// Member should disappear from list
		await expect(page.locator('table').getByText(inviteeEmail)).toHaveCount(0, { timeout: 5000 });

		// 7. Free plan limit test (3 members max: 1 current member + 2 invitations)
		// Invite #1 towards limit (total 2 used)
		const extraEmail1 = createUniqueEmail('extra1');
		await page.getByTestId('invite-member-btn').click();
		await page.getByTestId('invite-email-input').fill(extraEmail1);
		await page.getByTestId('invite-submit-btn').click();
		await expect(page.getByTestId('invite-dialog')).not.toBeVisible({ timeout: 10000 });

		// Invite #2 towards limit (total 3 used, reaching maximum)
		const extraEmail2 = createUniqueEmail('extra2');
		await page.getByTestId('invite-member-btn').click();
		await page.getByTestId('invite-email-input').fill(extraEmail2);
		await page.getByTestId('invite-submit-btn').click();
		await expect(page.getByTestId('invite-dialog')).not.toBeVisible({ timeout: 10000 });

		// Invite #3 exceeding limit -> should trigger UpgradeDialog
		const exceedingEmail = createUniqueEmail('exceeding');
		await page.getByTestId('invite-member-btn').click();
		await page.getByTestId('invite-email-input').fill(exceedingEmail);
		await page.getByTestId('invite-submit-btn').click();

		// UpgradeDialog should appear
		await expect(page.getByTestId('upgrade-dialog')).toBeVisible({ timeout: 10000 });
		const upgradeBtn = page.getByTestId('upgrade-dialog-btn');
		await expect(upgradeBtn).toBeVisible();
		expect(await upgradeBtn.getAttribute('href')).toContain(`/orgs/${orgId}/billing/plans`);
	});
});
