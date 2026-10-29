import { render } from 'vitest-browser-svelte';
import { describe, expect, it, vi } from 'vitest';
import DashboardPage from './+page.svelte';

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-test-123' },
		url: new URL('http://localhost:5173/orgs/org-test-123')
	}
}));

vi.mock('$lib/features/org/api', () => ({
	createOrganizationQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			organization: {
				id: 'org-test-123',
				name: 'Acme Rocket Corp',
				slug: 'acme-rocket',
				role: 'owner'
			}
		}
	})
}));

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			user: {
				id: 'user-owner-1',
				email: 'owner@example.com',
				email_verified: true,
				name: 'Owner User'
			}
		}
	})
}));

vi.mock('$lib/features/billing/api', () => ({
	createSubscriptionQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			plan_id: 'plan-pro',
			effective_plan: 'pro',
			status: 'active',
			current_period_end: '2026-12-31T00:00:00Z',
			usage: {
				members: { used: 3, max: 10 }
			}
		}
	}),
	createInvoicesQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			items: [
				{
					id: 'inv-1',
					number: 'INV-001',
					amount: 99000,
					currency: 'IDR',
					status: 'open',
					due_at: '2026-11-15T00:00:00Z'
				}
			]
		}
	})
}));

describe('Dashboard summary page (+page.svelte)', () => {
	it('renders org title, plan card, members card, unpaid invoice card, and domain empty state', async () => {
		const screen = render(DashboardPage);

		await expect.element(screen.getByTestId('dashboard-summary')).toBeVisible();
		await expect.element(screen.getByText('Acme Rocket Corp')).toBeVisible();
		await expect.element(screen.getByTestId('dashboard-plan-card')).toBeVisible();
		await expect.element(screen.getByTestId('dashboard-members-card')).toBeVisible();
		await expect.element(screen.getByTestId('dashboard-unpaid-invoice-card')).toBeVisible();
	});
});
