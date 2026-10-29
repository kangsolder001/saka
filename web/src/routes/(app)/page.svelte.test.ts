import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from 'vitest-browser-svelte';
import RootAppPage from './+page.svelte';
import { LAST_ORG_ID_KEY } from '$lib/features/org/api';
import { goto } from '$app/navigation';

let mockOrganizations: Array<{ id: string; name: string; role: string }> = [];

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		isSuccess: true,
		data: {
			user: { id: 'u1', name: 'User', email: 'u@example.com' },
			organizations: mockOrganizations
		}
	})
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

describe('RootAppPage', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		localStorage.clear();
	});

	it('redirects to lastOrgId if it exists in me.organizations', async () => {
		mockOrganizations = [
			{ id: 'org-1', name: 'Org 1', role: 'member' },
			{ id: 'org-2', name: 'Org 2', role: 'owner' }
		];
		localStorage.setItem(LAST_ORG_ID_KEY, 'org-2');

		render(RootAppPage);

		await vi.waitFor(() => {
			expect(goto).toHaveBeenCalledWith('/orgs/org-2');
		});
	});

	it('redirects to first organization if lastOrgId is not in organizations', async () => {
		mockOrganizations = [{ id: 'org-1', name: 'Org 1', role: 'member' }];
		localStorage.setItem(LAST_ORG_ID_KEY, 'other-org');

		render(RootAppPage);

		await vi.waitFor(() => {
			expect(goto).toHaveBeenCalledWith('/orgs/org-1');
		});
	});

	it('redirects to /orgs/new if user has no organizations', async () => {
		mockOrganizations = [];
		localStorage.removeItem(LAST_ORG_ID_KEY);

		render(RootAppPage);

		await vi.waitFor(() => {
			expect(goto).toHaveBeenCalledWith('/orgs/new');
		});
	});
});
