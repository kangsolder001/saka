import { page } from 'vitest/browser';
import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import OrgSwitcher from './OrgSwitcher.svelte';

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		data: {
			organizations: [
				{ id: 'org-1', name: 'Nusantara Tech', slug: 'nusantara', role: 'owner' },
				{ id: 'org-2', name: 'Garuda Digital', slug: 'garuda', role: 'member' }
			]
		}
	})
}));

describe('OrgSwitcher', () => {
	it('renders trigger button with current organization name', async () => {
		render(OrgSwitcher, {
			activeOrgId: 'org-1'
		});

		await expect.element(page.getByTestId('org-switcher-trigger')).toBeInTheDocument();
		await expect.element(page.getByText('Nusantara Tech')).toBeInTheDocument();
	});
});
