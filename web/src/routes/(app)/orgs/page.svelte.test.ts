import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import OrgsPage from './+page.svelte';
import * as m from '$lib/paraglide/messages';
import type { paths } from '$lib/api/types';

type GetMeResponse = paths['/me']['get']['responses']['200']['content']['application/json'];

let mockQueryResult = {
	isPending: false,
	isError: false,
	error: null as Error | null,
	data: {
		user: {
			id: 'u1',
			name: 'User 1',
			email: 'user@example.com',
			avatar_url: null,
			email_verified: true
		},
		organizations: [] as Array<{
			id: string;
			name: string;
			slug: string;
			role: 'owner' | 'admin' | 'member';
		}>
	} as unknown as GetMeResponse,
	refetch: vi.fn()
};

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => mockQueryResult
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

describe('OrgsPage (+page.svelte)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockQueryResult = {
			isPending: false,
			isError: false,
			error: null,
			data: {
				user: {
					id: 'u1',
					name: 'User 1',
					email: 'user@example.com',
					avatar_url: null,
					email_verified: true
				},
				organizations: []
			} as unknown as GetMeResponse,
			refetch: vi.fn()
		};
	});

	it('renders empty state when user has no organizations', async () => {
		mockQueryResult.data.organizations = [];

		const screen = render(OrgsPage);

		expect(screen.getByText(m.orgs_list_title())).toBeDefined();
		expect(screen.getByText(m.orgs_list_empty_title())).toBeDefined();
		expect(screen.getByTestId('empty-create-org-btn')).toBeDefined();
	});

	it('renders organizations list when organizations exist', async () => {
		mockQueryResult.data.organizations = [
			{ id: 'org-1', name: 'Acme Corp', slug: 'acme-corp', role: 'owner' },
			{ id: 'org-2', name: 'Beta Labs', slug: 'beta-labs', role: 'member' }
		];

		const screen = render(OrgsPage);

		expect(screen.getByText(m.orgs_list_title())).toBeDefined();
		expect(screen.getByText('Acme Corp')).toBeDefined();
		expect(screen.getByText('acme-corp')).toBeDefined();
		expect(screen.getByText('Beta Labs')).toBeDefined();
		expect(screen.getByText('beta-labs')).toBeDefined();

		const acmeLink = screen.getByTestId('org-link-org-1');
		expect(acmeLink.element().getAttribute('href')).toBe('/orgs/org-1');

		const betaLink = screen.getByTestId('org-link-org-2');
		expect(betaLink.element().getAttribute('href')).toBe('/orgs/org-2');
	});

	it('renders skeleton during loading state', async () => {
		mockQueryResult.isPending = true;

		const screen = render(OrgsPage);

		expect(screen.getByTestId('page-skeleton')).toBeDefined();
	});

	it('renders error state when query fails', async () => {
		mockQueryResult.isPending = false;
		mockQueryResult.isError = true;
		mockQueryResult.error = new Error('Network error');

		const screen = render(OrgsPage);

		expect(screen.getByTestId('error-state')).toBeDefined();
	});
});
