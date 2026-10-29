import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import PlansPage from './+page.svelte';
import type { Plan, Subscription, CheckoutResponse } from '$lib/api/types';

const mockPlans: Plan[] = [
	{
		code: 'free',
		name: 'Free',
		price_amount: 0,
		currency: 'IDR',
		interval: 'month',
		trial_days: 0,
		features: [],
		limits: { members: 3 }
	},
	{
		code: 'pro',
		name: 'Pro',
		price_amount: 99000,
		currency: 'IDR',
		interval: 'month',
		trial_days: 14,
		features: ['export_csv'],
		limits: { members: 10 }
	}
];

const mockSub: Subscription = {
	plan: 'free',
	status: 'active',
	trial_ends_at: null,
	current_period_start: '2026-01-01T00:00:00Z',
	current_period_end: null,
	cancel_at_period_end: false,
	pending_plan: null,
	grace_until: null,
	effective_plan: 'free',
	usage: { members: { used: 1, max: 3 } }
};

const mockCheckout = vi.fn();
const mockChangePlan = vi.fn();
const mockHandlePurchaseResult = vi.fn();

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-test-123' },
		url: new URL('http://localhost:3000/orgs/org-test-123/billing/plans')
	}
}));

vi.mock('$lib/features/billing/api', () => ({
	createPlansQuery: () => ({
		isPending: false,
		isError: false,
		data: mockPlans,
		refetch: vi.fn()
	}),
	createSubscriptionQuery: () => ({
		isPending: false,
		isError: false,
		data: mockSub,
		refetch: vi.fn()
	}),
	checkout: (...args: unknown[]) => mockCheckout(...args),
	changePlan: (...args: unknown[]) => mockChangePlan(...args)
}));

vi.mock('$lib/features/org/api', () => ({
	createOrganizationQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			organization: {
				id: 'org-test-123',
				name: 'Test Org',
				slug: 'test-org',
				role: 'owner'
			}
		},
		refetch: vi.fn()
	})
}));

vi.mock('$lib/features/billing/purchase', () => ({
	handlePurchaseResult: (...args: unknown[]) => mockHandlePurchaseResult(...args)
}));

vi.mock('@tanstack/svelte-query', async (importOriginal) => {
	const actual = await importOriginal<typeof import('@tanstack/svelte-query')>();
	return {
		...actual,
		useQueryClient: () => ({
			invalidateQueries: vi.fn().mockResolvedValue(undefined)
		})
	};
});

describe('Plans Page (+page.svelte)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders plans page with grid of PlanCards', async () => {
		const screen = render(PlansPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('plans-page')).toBeDefined();
			expect(screen.getByTestId('plans-grid')).toBeDefined();
			expect(screen.getByTestId('plan-card-free')).toBeDefined();
			expect(screen.getByTestId('plan-card-pro')).toBeDefined();
		});

		expect(screen.getByText('Pilihan Paket')).toBeDefined();
	});

	it('executes checkout when selecting Pro plan and confirming', async () => {
		const checkoutResult: CheckoutResponse = {
			trial_started: true,
			subscription: { ...mockSub, plan: 'pro' },
			invoice: {
				id: 'inv-1',
				number: 'INV-001',
				kind: 'checkout',
				plan: 'pro',
				plan_name: 'Pro',
				amount: 99000,
				currency: 'IDR',
				status: 'open',
				period_start: '2026-01-01T00:00:00Z',
				period_end: '2026-02-01T00:00:00Z',
				due_at: '2026-01-02T00:00:00Z',
				paid_at: null,
				payment_url: 'https://pay.fake.test/INV-001',
				created_at: '2026-01-01T00:00:00Z'
			}
		};
		mockCheckout.mockResolvedValue(checkoutResult);

		const screen = render(PlansPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('plan-card-pro')).toBeDefined();
		});

		// Find the Pro action button
		const proCard = screen.getByTestId('plan-card-pro');
		const actionBtn = proCard
			.element()
			.querySelector('[data-testid="plan-action-btn"]') as HTMLElement;
		expect(actionBtn).toBeDefined();
		actionBtn.click();

		// Confirm dialog should open
		await vi.waitFor(() => {
			expect(screen.getByTestId('plan-confirm-dialog')).toBeDefined();
		});

		const proceedBtn = screen.getByTestId('confirm-dialog-proceed');
		await proceedBtn.click();

		await vi.waitFor(() => {
			expect(mockCheckout).toHaveBeenCalledWith('org-test-123', 'pro');
			expect(mockHandlePurchaseResult).toHaveBeenCalledWith(
				checkoutResult,
				'org-test-123',
				expect.anything()
			);
		});
	});
});
