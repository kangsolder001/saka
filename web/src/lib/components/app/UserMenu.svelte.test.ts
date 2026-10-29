import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import UserMenu from './UserMenu.svelte';
import { setSession } from '$lib/auth/session.svelte';

describe('UserMenu', () => {
	it('renders user initials and name in trigger', async () => {
		setSession({
			access_token: 'fake',
			token_type: 'Bearer',
			expires_in: 900,
			user: {
				id: 'u1',
				name: 'Budi Santoso',
				email: 'budi@example.com',
				email_verified: true
			}
		});

		render(UserMenu);

		await expect.element(page.getByTestId('user-menu-trigger')).toBeInTheDocument();
		await expect.element(page.getByText('Budi Santoso')).toBeInTheDocument();
		await expect.element(page.getByText('BS')).toBeInTheDocument();
	});
});
