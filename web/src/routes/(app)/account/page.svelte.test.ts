import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ProfilePage from './+page.svelte';

const mockUpdateMe = vi.fn();

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			user: {
				id: 'u1',
				name: 'Initial User',
				email: 'user@example.com',
				avatar_url: null,
				email_verified: true
			},
			organizations: []
		}
	}),
	updateMe: (params: unknown) => mockUpdateMe(params)
}));

describe('account profile page', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders profile data and saves updated name', async () => {
		mockUpdateMe.mockResolvedValue({
			user: {
				id: 'u1',
				name: 'Updated Name',
				email: 'user@example.com',
				avatar_url: null,
				email_verified: true
			}
		});

		const screen = render(ProfilePage);

		// Wait for user name to be populated
		await vi.waitFor(() => {
			const nameInput = screen.getByLabelText(/Full Name|Nama Lengkap/i);
			expect((nameInput.element() as HTMLInputElement).value).toBe('Initial User');
		});

		const nameInput = screen.getByLabelText(/Full Name|Nama Lengkap/i);
		const saveButton = screen.getByRole('button', { name: /Save Changes|Simpan Perubahan/i });

		// Change name and click submit
		await nameInput.fill('Updated Name');
		await saveButton.click();

		await vi.waitFor(() => {
			expect(mockUpdateMe).toHaveBeenCalledWith({
				name: 'Updated Name'
			});
		});
	});
});
