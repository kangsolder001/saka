import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import SettingsPage from './+page.svelte';
import type { Member, Role } from '$lib/api/types';
import {
	deleteOrganization,
	leaveOrganization,
	transferOwnership,
	updateOrganization
} from '$lib/features/org/api';
import { goto } from '$app/navigation';

const { mockState } = vi.hoisted(() => {
	const mockOrg = {
		id: 'org-test-123',
		name: 'Acme Corp',
		slug: 'acme-corp',
		logo_url: 'https://example.com/logo.png',
		owner_id: 'user-owner-id',
		role: 'owner' as Role,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-01T00:00:00Z'
	};

	const mockMembers: Member[] = [
		{
			user_id: 'user-owner-id',
			name: 'Boss Owner',
			email: 'owner@example.com',
			avatar_url: null,
			role: 'owner',
			joined_at: '2026-01-01T00:00:00Z'
		},
		{
			user_id: '00000000-0000-0000-0000-000000000002',
			name: 'Alice Employee',
			email: 'alice@example.com',
			avatar_url: null,
			role: 'member',
			joined_at: '2026-01-02T00:00:00Z'
		}
	];

	return {
		mockState: {
			currentRole: 'owner' as Role,
			mockOrg,
			mockMembers
		}
	};
});

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-test-123' },
		url: new URL('http://localhost:3000/orgs/org-test-123/settings')
	}
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			user: {
				id: 'user-owner-id',
				name: 'Boss Owner',
				email: 'owner@example.com'
			},
			organizations: []
		}
	})
}));

vi.mock('$lib/features/org/api', async (importOriginal) => {
	const actual = await importOriginal<typeof import('$lib/features/org/api')>();
	return {
		...actual,
		createOrganizationQuery: () => ({
			isPending: false,
			isError: false,
			data: {
				organization: { ...mockState.mockOrg, role: mockState.currentRole }
			}
		}),
		createMembersQuery: () => ({
			isPending: false,
			isError: false,
			data: mockState.mockMembers
		}),
		updateOrganization: vi.fn().mockResolvedValue({
			organization: { ...mockState.mockOrg, name: 'Acme Updated' }
		}),
		transferOwnership: vi.fn().mockResolvedValue(undefined),
		deleteOrganization: vi.fn().mockResolvedValue(undefined),
		leaveOrganization: vi.fn().mockResolvedValue(undefined),
		clearLastOrgId: vi.fn()
	};
});

describe('Organization Settings Page', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockState.currentRole = 'owner';
	});

	it('renders general settings form, transfer card, and delete card for owner', async () => {
		mockState.currentRole = 'owner';
		const screen = render(SettingsPage);

		// Header
		await vi.waitFor(() => {
			const nameInput = screen.getByLabelText(/Name|Nama/i);
			expect((nameInput.element() as HTMLInputElement).value).toBe('Acme Corp');
		});

		// Name input is enabled
		const nameInput = screen.getByLabelText(/Name|Nama/i);
		expect((nameInput.element() as HTMLInputElement).disabled).toBe(false);

		// Save button exists
		const saveBtn = screen.getByRole('button', { name: /Save Changes|Simpan Perubahan/i });
		expect(saveBtn).toBeDefined();

		// Transfer card and delete card exist
		expect(
			screen.getByRole('heading', {
				name: /Transfer Ownership|Transfer Kepemilikan/i
			})
		).toBeDefined();

		expect(
			screen.getByRole('heading', {
				name: /Delete Organization|Hapus Organisasi/i
			})
		).toBeDefined();

		// Leave card does NOT exist for owner
		await expect
			.element(
				screen.getByRole('heading', {
					name: /Leave Organization|Keluar dari Organisasi/i
				})
			)
			.not.toBeInTheDocument();
	});

	it('submits updated organization name when changed by owner', async () => {
		mockState.currentRole = 'owner';
		const screen = render(SettingsPage);

		await vi.waitFor(() => {
			const nameInput = screen.getByLabelText(/Name|Nama/i);
			expect((nameInput.element() as HTMLInputElement).value).toBe('Acme Corp');
		});

		const nameInput = screen.getByLabelText(/Name|Nama/i);
		await nameInput.fill('Acme Corp Renamed');

		const saveBtn = screen.getByRole('button', { name: /Save Changes|Simpan Perubahan/i });
		await saveBtn.click();

		await vi.waitFor(() => {
			expect(updateOrganization).toHaveBeenCalledWith('org-test-123', {
				name: 'Acme Corp Renamed'
			});
		});
	});

	it('disables general form inputs and shows leave card for member role', async () => {
		mockState.currentRole = 'member';
		const screen = render(SettingsPage);

		await vi.waitFor(() => {
			const nameInput = screen.getByLabelText(/Name|Nama/i);
			expect((nameInput.element() as HTMLInputElement).value).toBe('Acme Corp');
		});

		const nameInput = screen.getByLabelText(/Name|Nama/i);
		expect((nameInput.element() as HTMLInputElement).disabled).toBe(true);

		// Save button does NOT exist
		await expect
			.element(screen.getByRole('button', { name: /Save Changes|Simpan Perubahan/i }))
			.not.toBeInTheDocument();

		// Transfer and delete do NOT exist
		await expect
			.element(screen.getByRole('heading', { name: /Transfer Ownership|Transfer Kepemilikan/i }))
			.not.toBeInTheDocument();
		await expect
			.element(screen.getByRole('heading', { name: /Delete Organization|Hapus Organisasi/i }))
			.not.toBeInTheDocument();

		// Leave card DOES exist
		expect(
			screen.getByRole('heading', { name: /Leave Organization|Keluar dari Organisasi/i })
		).toBeDefined();
	});

	it('allows editing general settings and shows leave card but not delete card for admin role', async () => {
		mockState.currentRole = 'admin';
		const screen = render(SettingsPage);

		await vi.waitFor(() => {
			const nameInput = screen.getByLabelText(/Name|Nama/i);
			expect((nameInput.element() as HTMLInputElement).value).toBe('Acme Corp');
		});

		const nameInput = screen.getByLabelText(/Name|Nama/i);
		expect((nameInput.element() as HTMLInputElement).disabled).toBe(false);

		// Save button exists
		expect(screen.getByRole('button', { name: /Save Changes|Simpan Perubahan/i })).toBeDefined();

		// Transfer and delete do NOT exist
		await expect
			.element(screen.getByRole('heading', { name: /Transfer Ownership|Transfer Kepemilikan/i }))
			.not.toBeInTheDocument();
		await expect
			.element(screen.getByRole('heading', { name: /Delete Organization|Hapus Organisasi/i }))
			.not.toBeInTheDocument();

		// Leave card DOES exist
		expect(
			screen.getByRole('heading', { name: /Leave Organization|Keluar dari Organisasi/i })
		).toBeDefined();
	});

	it('transfers ownership when target member and current password are submitted', async () => {
		mockState.currentRole = 'owner';
		const screen = render(SettingsPage);

		await vi.waitFor(() => {
			const memberSelect = screen.getByLabelText(/Select a member|Pilih anggota/i);
			expect(memberSelect).toBeDefined();
		});

		const memberSelect = screen.getByLabelText(/Select a member|Pilih anggota/i);
		const selectEl = memberSelect.element() as HTMLSelectElement;
		selectEl.value = '00000000-0000-0000-0000-000000000002';
		selectEl.dispatchEvent(new Event('change', { bubbles: true }));

		const passInput = screen.getByLabelText(/Current Password|Kata Sandi Saat Ini/i);
		await passInput.fill('mySecretPass');

		const transferBtn = screen.getByRole('button', {
			name: /Transfer Ownership|Transfer Kepemilikan/i
		});
		await transferBtn.click();

		await vi.waitFor(() => {
			expect(transferOwnership).toHaveBeenCalledWith('org-test-123', {
				user_id: '00000000-0000-0000-0000-000000000002',
				current_password: 'mySecretPass'
			});
		});
	});

	it('leaves organization upon confirmation dialog', async () => {
		mockState.currentRole = 'member';
		const screen = render(SettingsPage);

		await vi.waitFor(() => {
			const leaveBtn = screen.getByRole('button', {
				name: /Leave Organization|Keluar dari Organisasi/i
			});
			expect(leaveBtn).toBeDefined();
		});

		const leaveBtn = screen.getByRole('button', {
			name: /Leave Organization|Keluar dari Organisasi/i
		});
		await leaveBtn.click();

		// Wait for alertdialog
		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', {
			name: /Leave Organization|Keluar dari Organisasi/i
		});
		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(leaveOrganization).toHaveBeenCalledWith('org-test-123');
			expect(goto).toHaveBeenCalledWith('/');
		});
	});

	it('requires typing exact org name before deleting organization', async () => {
		mockState.currentRole = 'owner';
		const screen = render(SettingsPage);

		await vi.waitFor(() => {
			const deleteBtn = screen.getByRole('button', {
				name: /Delete Organization|Hapus Organisasi/i
			});
			expect(deleteBtn).toBeDefined();
		});

		const deleteBtn = screen.getByRole('button', {
			name: /Delete Organization|Hapus Organisasi/i
		});
		await deleteBtn.click();

		// Wait for dialog
		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', {
			name: /Delete Organization|Hapus Organisasi/i
		});

		// Confirm button should be disabled initially
		expect((confirmBtn.element() as HTMLButtonElement).disabled).toBe(true);

		// Type org name into confirmation input
		const typedInput = dialog.getByRole('textbox');
		await typedInput.fill('Acme Corp');

		// Confirm button should now be enabled
		await vi.waitFor(() => {
			expect((confirmBtn.element() as HTMLButtonElement).disabled).toBe(false);
		});

		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(deleteOrganization).toHaveBeenCalledWith('org-test-123');
			expect(goto).toHaveBeenCalledWith('/');
		});
	});
});
