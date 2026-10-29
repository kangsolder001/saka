import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import InviteDialogTestWrapper from './InviteDialogTestWrapper.svelte';
import { createInvitation } from '../api';
import { upgradeDialog } from '$lib/features/billing/upgrade.svelte';
import { ApiError } from '$lib/api/errors';

vi.mock('../api', () => ({
	createInvitation: vi.fn()
}));

describe('InviteDialog', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		upgradeDialog.open = false;
		upgradeDialog.orgId = '';
	});

	it('renders form fields when open', async () => {
		const screen = render(InviteDialogTestWrapper, {
			open: true,
			orgId: 'org-test'
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('invite-dialog')).toBeDefined();
			expect(screen.getByTestId('invite-email-input')).toBeDefined();
			expect(screen.getByTestId('invite-role-trigger')).toBeDefined();
			expect(screen.getByTestId('invite-submit-btn')).toBeDefined();
		});
	});

	it('submits invitation successfully', async () => {
		vi.mocked(createInvitation).mockResolvedValue({
			invitation: {
				id: 'inv-1',
				email: 'newuser@example.com',
				role: 'member',
				invited_by: 'u-1',
				expires_at: '2026-02-01T00:00:00Z',
				created_at: '2026-01-25T00:00:00Z'
			},
			status: 201
		});

		const onSuccess = vi.fn();
		const screen = render(InviteDialogTestWrapper, {
			open: true,
			orgId: 'org-test',
			onSuccess
		});

		const input = screen.getByTestId('invite-email-input');
		await input.fill('newuser@example.com');

		const submitBtn = screen.getByTestId('invite-submit-btn');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(createInvitation).toHaveBeenCalledWith('org-test', {
				email: 'newuser@example.com',
				role: 'member'
			});
			expect(onSuccess).toHaveBeenCalled();
		});
	});

	it('handles 409 already member conflict', async () => {
		vi.mocked(createInvitation).mockRejectedValue(
			new ApiError({
				status: 409,
				code: 'CONFLICT',
				serverMessage: 'Already member',
				details: {}
			})
		);

		const screen = render(InviteDialogTestWrapper, {
			open: true,
			orgId: 'org-test'
		});

		const input = screen.getByTestId('invite-email-input');
		await input.fill('existing@example.com');

		const submitBtn = screen.getByTestId('invite-submit-btn');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(screen.getByTestId('invite-email-error')).toBeDefined();
		});
	});

	it('handles 402 plan limit reached by opening upgrade dialog', async () => {
		vi.mocked(createInvitation).mockRejectedValue(
			new ApiError({
				status: 402,
				code: 'PLAN_LIMIT_REACHED',
				serverMessage: 'Limit reached',
				details: {}
			})
		);

		const screen = render(InviteDialogTestWrapper, {
			open: true,
			orgId: 'org-test'
		});

		const input = screen.getByTestId('invite-email-input');
		await input.fill('new@example.com');

		const submitBtn = screen.getByTestId('invite-submit-btn');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(upgradeDialog.open).toBe(true);
			expect(upgradeDialog.orgId).toBe('org-test');
		});
	});
});
