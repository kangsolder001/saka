import { page } from 'vitest/browser';
import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import AppShell from './AppShell.svelte';

vi.mock('$app/state', () => ({
	page: {
		url: new URL('http://localhost:3000/orgs/org-123')
	}
}));

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		data: {
			organizations: [{ id: 'org-123', name: 'Acme Corp', slug: 'acme', role: 'owner' }]
		}
	})
}));

describe('AppShell', () => {
	it('renders desktop sidebar and mobile header', async () => {
		render(AppShell, {
			activeOrgId: 'org-123'
		});

		await expect.element(page.getByTestId('desktop-sidebar')).toBeInTheDocument();
		await expect.element(page.getByTestId('mobile-header')).toBeInTheDocument();
	});
});
