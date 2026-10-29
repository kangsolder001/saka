import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import MemberRowTestWrapper from './MemberRowTestWrapper.svelte';
import { removeMember } from '../api';
import type { Member } from '$lib/api/types';

vi.mock('../api', () => ({
	changeMemberRole: vi.fn(),
	removeMember: vi.fn()
}));

const mockMember: Member = {
	user_id: 'u-1',
	email: 'john@example.com',
	name: 'John Doe',
	avatar_url: null,
	role: 'member',
	joined_at: '2026-01-15T10:00:00Z'
};

describe('MemberRow', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders member info and self label when member is current user', async () => {
		const screen = render(MemberRowTestWrapper, {
			member: mockMember,
			callerRole: 'admin',
			selfUserId: 'u-1',
			orgId: 'org-1'
		});

		expect(screen.getByText('John Doe')).toBeDefined();
		expect(screen.getByText('john@example.com')).toBeDefined();
		expect(screen.getByTestId('self-badge')).toBeDefined();
	});

	it('renders role select and remove button for admin viewing another member', async () => {
		const otherMember: Member = {
			...mockMember,
			user_id: 'u-2',
			email: 'other@example.com',
			name: 'Other User'
		};

		const screen = render(MemberRowTestWrapper, {
			member: otherMember,
			callerRole: 'admin',
			selfUserId: 'u-admin',
			orgId: 'org-1'
		});

		expect(screen.getByTestId('role-select-trigger-u-2')).toBeDefined();
		expect(screen.getByTestId('remove-member-btn-u-2')).toBeDefined();
	});

	it('does not render remove or role change for regular member viewing another user', async () => {
		const otherMember: Member = {
			...mockMember,
			user_id: 'u-2',
			email: 'other@example.com',
			name: 'Other User'
		};

		const screen = render(MemberRowTestWrapper, {
			member: otherMember,
			callerRole: 'member',
			selfUserId: 'u-member',
			orgId: 'org-1'
		});

		expect(screen.getByText('Other User')).toBeDefined();
		expect(screen.getByText('other@example.com')).toBeDefined();
		// Button to remove and role select should not exist
		expect(document.querySelector('[data-testid="remove-member-btn-u-2"]')).toBeNull();
		expect(document.querySelector('[data-testid="role-select-trigger-u-2"]')).toBeNull();
	});

	it('removes member when remove confirmed', async () => {
		vi.mocked(removeMember).mockResolvedValue(undefined);
		const onChanged = vi.fn();

		const otherMember: Member = {
			...mockMember,
			user_id: 'u-3',
			email: 'removeme@example.com',
			name: 'Remove Me'
		};

		const screen = render(MemberRowTestWrapper, {
			member: otherMember,
			callerRole: 'owner',
			selfUserId: 'u-owner',
			orgId: 'org-1',
			onChanged
		});

		const removeBtn = screen.getByTestId('remove-member-btn-u-3');
		await removeBtn.click();

		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', { name: /Remove|Hapus|Keluarkan/i });
		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(removeMember).toHaveBeenCalledWith('org-1', 'u-3');
			expect(onChanged).toHaveBeenCalled();
		});
	});
});
