import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import BillingOverviewPage from './+page.svelte';
import type { Subscription } from '$lib/api/types';
import { cancelSubscription, resumeSubscription } from '$lib/features/billing/api';
import { setLocale } from '$lib/i18n';
import { toast } from 'svelte-sonner';

const mockState = {
	role: 'owner' as 'owner' | 'admin' | 'member',
	sub: null as Subscription | null,
	isPending: false,
	isError: false,
	error: null as Error | null
};

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-test-123' },
		url: new URL('http://localhost:5173/orgs/org-test-123/billing')
	}
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
				role: mockState.role
			}
		}
	})
}));

vi.mock('$lib/features/billing/api', async (importOriginal) => {
	const actual = await importOriginal<typeof import('$lib/features/billing/api')>();
	return {
		...actual,
		createSubscriptionQuery: () => ({
			isPending: mockState.isPending,
			isError: mockState.isError,
			error: mockState.error,
			data: mockState.sub,
			refetch: vi.fn()
		}),
		cancelSubscription: vi.fn(),
		resumeSubscription: vi.fn()
	};
});

vi.mock('svelte-sonner', () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn()
	}
}));

describe('Billing Overview Page (+page.svelte)', () => {
	const freeSub: Subscription = {
		plan: 'free',
		status: 'active',
		trial_ends_at: null,
		current_period_start: '2026-01-01T00:00:00Z',
		current_period_end: null,
		cancel_at_period_end: false,
		pending_plan: null,
		grace_until: null,
		effective_plan: 'free',
		usage: {
			members: { used: 2, max: 5 }
		}
	};

	const paidSub: Subscription = {
		plan: 'pro',
		status: 'active',
		trial_ends_at: null,
		current_period_start: '2026-01-01T00:00:00Z',
		current_period_end: '2026-02-01T00:00:00Z',
		cancel_at_period_end: false,
		pending_plan: null,
		grace_until: null,
		effective_plan: 'pro',
		usage: {
			members: { used: 8, max: 20 },
			storage: { used: 50, max: 100 }
		}
	};

	beforeEach(() => {
		vi.clearAllMocks();
		setLocale('id');
		mockState.role = 'owner';
		mockState.sub = freeSub;
		mockState.isPending = false;
		mockState.isError = false;
		mockState.error = null;
	});

	it('renders free plan without cancel button', async () => {
		const screen = render(BillingOverviewPage);

		expect(screen.getByTestId('billing-plan-name').element().textContent?.trim()).toBe('free');
		expect(screen.getByTestId('billing-change-plan-btn')).toBeDefined();
		expect(document.querySelector('[data-testid="billing-cancel-btn"]')).toBeNull();
		expect(screen.getByTestId('billing-usage-card')).toBeDefined();
		expect(screen.getByText('Anggota')).toBeDefined();
	});

	it('renders paid plan with cancel button for owner', async () => {
		mockState.sub = paidSub;
		const screen = render(BillingOverviewPage);

		expect(screen.getByTestId('billing-plan-name').element().textContent?.trim()).toBe('pro');
		expect(screen.getByTestId('billing-change-plan-btn')).toBeDefined();
		expect(screen.getByTestId('billing-cancel-btn')).toBeDefined();
		expect(screen.getByTestId('billing-period')).toBeDefined();
	});

	it('does not render owner action buttons for non-owner', async () => {
		mockState.sub = paidSub;
		mockState.role = 'member';
		render(BillingOverviewPage);

		expect(document.querySelector('[data-testid="billing-change-plan-btn"]')).toBeNull();
		expect(document.querySelector('[data-testid="billing-cancel-btn"]')).toBeNull();
	});

	it('opens confirm dialog and calls cancelSubscription upon confirmation', async () => {
		vi.mocked(cancelSubscription).mockResolvedValue({
			...paidSub,
			cancel_at_period_end: true
		});

		mockState.sub = paidSub;
		const screen = render(BillingOverviewPage);

		const cancelBtn = screen.getByTestId('billing-cancel-btn');
		await cancelBtn.click();

		// Dialog should be open
		expect(screen.getByText('Batalkan Langganan')).toBeDefined();
		expect(screen.getByText('tanpa refund')).toBeDefined();

		// Click confirm button
		const confirmBtn = screen.getByRole('button', { name: 'Ya, batalkan' });
		await confirmBtn.click();

		expect(cancelSubscription).toHaveBeenCalledWith('org-test-123');
		expect(toast.success).toHaveBeenCalled();
	});

	it('renders resume button when cancel_at_period_end is true and calls resumeSubscription', async () => {
		vi.mocked(resumeSubscription).mockResolvedValue({
			...paidSub,
			cancel_at_period_end: false
		});

		mockState.sub = {
			...paidSub,
			cancel_at_period_end: true
		};
		const screen = render(BillingOverviewPage);

		expect(document.querySelector('[data-testid="billing-cancel-btn"]')).toBeNull();
		const resumeBtn = screen.getByTestId('billing-resume-btn');
		expect(resumeBtn).toBeDefined();

		await resumeBtn.click();
		expect(resumeSubscription).toHaveBeenCalledWith('org-test-123');
		expect(toast.success).toHaveBeenCalled();
	});
});
