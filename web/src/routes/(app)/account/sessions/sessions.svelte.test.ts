import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import SessionsPage from './+page.svelte';
import { logout } from '$lib/auth/logout';
import { revokeSession } from '$lib/features/account/api';

const mockSessions = [
	{
		id: 's-current',
		user_agent:
			'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36',
		ip: '192.168.1.1',
		created_at: '2026-01-01T00:00:00Z',
		last_used_at: '2026-01-01T12:00:00Z',
		expires_at: '2026-01-08T00:00:00Z',
		current: true
	},
	{
		id: 's-other',
		user_agent:
			'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1',
		ip: '10.0.0.2',
		created_at: '2026-01-02T00:00:00Z',
		last_used_at: '2026-01-02T10:00:00Z',
		expires_at: '2026-01-09T00:00:00Z',
		current: false
	}
];

vi.mock('$lib/features/account/api', () => ({
	createSessionsQuery: () => ({
		isPending: false,
		isError: false,
		data: mockSessions,
		refetch: vi.fn()
	}),
	revokeSession: vi.fn()
}));

vi.mock('$lib/auth/logout', () => ({
	logout: vi.fn()
}));

describe('account sessions page', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders sessions and revokes another session', async () => {
		vi.mocked(revokeSession).mockResolvedValue(undefined);

		const screen = render(SessionsPage);

		await vi.waitFor(() => {
			expect(screen.getByText('Chrome on macOS')).toBeDefined();
			expect(screen.getByText('Safari on iOS')).toBeDefined();
			expect(screen.getByText(/Current Session|Sesi Ini/i)).toBeDefined();
		});

		// Click Revoke on the other session
		const otherRow = screen.getByTestId('session-row-s-other');
		const revokeBtn = otherRow.element().querySelector('button');
		expect(revokeBtn).not.toBeNull();
		revokeBtn!.click();

		// Confirm dialog should open
		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', { name: /Revoke|Cabut/i });
		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(revokeSession).toHaveBeenCalledWith('s-other');
		});
	});

	it('revoking current session triggers logout', async () => {
		vi.mocked(revokeSession).mockResolvedValue(undefined);
		vi.mocked(logout).mockResolvedValue(undefined);

		const screen = render(SessionsPage);

		await vi.waitFor(() => {
			expect(screen.getByText('Chrome on macOS')).toBeDefined();
		});

		const row = screen.getByTestId('session-row-s-current');
		const revokeBtn = row.element().querySelector('button');
		revokeBtn!.click();

		await vi.waitFor(() => {
			const dialog = screen.getByRole('alertdialog');
			expect(dialog).toBeDefined();
		});

		const dialog = screen.getByRole('alertdialog');
		const confirmBtn = dialog.getByRole('button', { name: /Revoke|Cabut/i });
		await confirmBtn.click();

		await vi.waitFor(() => {
			expect(revokeSession).toHaveBeenCalledWith('s-current');
			expect(logout).toHaveBeenCalled();
		});
	});
});
