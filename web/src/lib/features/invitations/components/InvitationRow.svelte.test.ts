import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import InvitationRowTestWrapper from './InvitationRowTestWrapper.svelte';
import { resendInvitation, revokeInvitation } from '../api';
import type { Invitation } from '$lib/api/types';

vi.mock('../api', () => ({
	resendInvitation: vi.fn(),
	revokeInvitation: vi.fn()
}));

const mockInvitation: Invitation = {
	id: 'inv-42',
	email: 'guest@example.com',
	role: 'member',
	invited_by: 'u-1',
	expires_at: '2026-02-05T00:00:00Z',
	created_at: '2026-01-29T00:00:00Z'
};

describe('InvitationRow', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders invitation email, role badge and expiry', async () => {
		const screen = render(InvitationRowTestWrapper, {
			invitation: mockInvitation,
			orgId: 'org-test'
		});

		expect(screen.getByText('guest@example.com')).toBeDefined();
		expect(screen.getByTestId('invitation-expiry')).toBeDefined();
		expect(screen.getByTestId('resend-invitation-btn-inv-42')).toBeDefined();
		expect(screen.getByTestId('revoke-invitation-btn-inv-42')).toBeDefined();
	});

	it('calls resendInvitation when resend button clicked', async () => {
		vi.mocked(resendInvitation).mockResolvedValue({ invitation: mockInvitation });
		const onChanged = vi.fn();

		const screen = render(InvitationRowTestWrapper, {
			invitation: mockInvitation,
			orgId: 'org-test',
			onChanged
		});

		const resendBtn = screen.getByTestId('resend-invitation-btn-inv-42');
		await resendBtn.click();

		await vi.waitFor(() => {
			expect(resendInvitation).toHaveBeenCalledWith('org-test', 'inv-42');
			expect(onChanged).toHaveBeenCalled();
		});
	});

	it('calls revokeInvitation when revoke is confirmed', async () => {
		vi.mocked(revokeInvitation).mockResolvedValue(undefined);
		const onChanged = vi.fn();

		const screen = render(InvitationRowTestWrapper, {
			invitation: mockInvitation,
			orgId: 'org-test',
			onChanged
		});

		const revokeBtn = screen.getByTestId('revoke-invitation-btn-inv-42');
		await revokeBtn.click();

		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', { name: /Revoke|Batalkan/i });
		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(revokeInvitation).toHaveBeenCalledWith('org-test', 'inv-42');
			expect(onChanged).toHaveBeenCalled();
		});
	});
});
