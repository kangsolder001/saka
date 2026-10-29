import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import DeleteAccountPage from './+page.svelte';
import { logout } from '$lib/auth/logout';
import { deleteMe } from '$lib/features/account/api';
import { ApiError } from '$lib/api/errors';

vi.mock('$lib/features/account/api', () => ({
	deleteMe: vi.fn()
}));

vi.mock('$lib/auth/logout', () => ({
	logout: vi.fn()
}));

describe('account delete page', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('deletes account successfully on confirmation', async () => {
		vi.mocked(deleteMe).mockResolvedValue(undefined);
		vi.mocked(logout).mockResolvedValue(undefined);

		const screen = render(DeleteAccountPage);

		const passwordInput = screen.getByLabelText(
			/Enter your password to confirm|Masukkan kata sandi untuk mengonfirmasi/i
		);
		const deleteBtn = screen.getByRole('button', {
			name: /Delete My Account|Hapus Akun Saya/i
		});

		await passwordInput.fill('secretPass123');
		await deleteBtn.click();

		// ConfirmDialog opens
		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', {
			name: /Delete My Account|Hapus Akun Saya/i
		});
		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(deleteMe).toHaveBeenCalledWith('secretPass123');
			expect(logout).toHaveBeenCalled();
		});
	});

	it('handles 409 owns_shared_organization error properly', async () => {
		vi.mocked(deleteMe).mockRejectedValue(
			new ApiError({
				status: 409,
				code: 'CONFLICT',
				serverMessage: 'User owns shared organization',
				details: { reason: 'owns_shared_organization' }
			})
		);

		const screen = render(DeleteAccountPage);

		const passwordInput = screen.getByLabelText(
			/Enter your password to confirm|Masukkan kata sandi untuk mengonfirmasi/i
		);
		const deleteBtn = screen.getByRole('button', {
			name: /Delete My Account|Hapus Akun Saya/i
		});

		await passwordInput.fill('secretPass123');
		await deleteBtn.click();

		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', {
			name: /Delete My Account|Hapus Akun Saya/i
		});
		await confirmBtn.click();

		await vi.waitFor(() => {
			const conflictAlert = screen.getByRole('alert');
			expect(conflictAlert).toBeDefined();
			expect(conflictAlert.element().textContent).toMatch(
				/tidak dapat menghapus akun|cannot delete your account/i
			);
			const transferLink = screen.getByRole('link', {
				name: /Buka Pengaturan Organisasi|Go to Organization Settings/i
			});
			expect(transferLink).toBeDefined();
		});
	});
});
