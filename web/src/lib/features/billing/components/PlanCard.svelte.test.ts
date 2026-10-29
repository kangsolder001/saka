import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import PlanCard from './PlanCard.svelte';
import type { Plan, Subscription } from '$lib/api/types';

const proPlan: Plan = {
	code: 'pro',
	name: 'Pro',
	price_amount: 99000,
	currency: 'IDR',
	interval: 'month',
	trial_days: 14,
	features: ['export_csv', 'audit_log'],
	limits: { members: 10 }
};

const freePlan: Plan = {
	code: 'free',
	name: 'Free',
	price_amount: 0,
	currency: 'IDR',
	interval: 'month',
	trial_days: 0,
	features: [],
	limits: { members: 3 }
};

const activeFreeSub: Subscription = {
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

const activeProSub: Subscription = {
	plan: 'pro',
	status: 'active',
	trial_ends_at: null,
	current_period_start: '2026-01-01T00:00:00Z',
	current_period_end: '2026-02-01T00:00:00Z',
	cancel_at_period_end: false,
	pending_plan: null,
	grace_until: null,
	effective_plan: 'pro',
	usage: { members: { used: 3, max: 10 } }
};

describe('PlanCard.svelte', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders plan details: name, formatted price, features, and limits', async () => {
		const screen = render(PlanCard, {
			plan: proPlan,
			subscription: activeFreeSub,
			role: 'owner',
			allPlans: [freePlan, proPlan]
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('plan-card-pro')).toBeDefined();
			expect(screen.getByTestId('plan-name')).toBeDefined();
		});

		expect(screen.getByText('Pro')).toBeDefined();
		expect(screen.getByText(/Rp\s*99\.000/)).toBeDefined();
		expect(screen.getByTestId('plan-features')).toBeDefined();
		expect(screen.getByTestId('plan-limits')).toBeDefined();
	});

	it('renders current plan badge if plan.code matches subscription.plan', async () => {
		const screen = render(PlanCard, {
			plan: proPlan,
			subscription: activeProSub,
			role: 'owner',
			allPlans: [freePlan, proPlan]
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('current-plan-badge')).toBeDefined();
		});
		expect(screen.getByText('Plan saat ini')).toBeDefined();
	});

	it('disables action button and shows note for non-owner role', async () => {
		const screen = render(PlanCard, {
			plan: proPlan,
			subscription: activeFreeSub,
			role: 'member',
			allPlans: [freePlan, proPlan]
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('only-owner-note')).toBeDefined();
		});

		const btn = screen.getByTestId('plan-action-btn');
		expect((btn.element() as HTMLButtonElement).disabled).toBe(true);
	});

	it('disables button when resolved action is none', async () => {
		const screen = render(PlanCard, {
			plan: proPlan,
			subscription: activeProSub,
			role: 'owner',
			allPlans: [freePlan, proPlan]
		});

		await vi.waitFor(() => {
			const btn = screen.getByTestId('plan-action-btn');
			expect((btn.element() as HTMLButtonElement).disabled).toBe(true);
		});
	});

	it('opens confirmation dialog on click and displays no-proration notice', async () => {
		const onSelectPlan = vi.fn().mockResolvedValue(undefined);
		const screen = render(PlanCard, {
			plan: proPlan,
			subscription: activeFreeSub,
			role: 'owner',
			allPlans: [freePlan, proPlan],
			onSelectPlan
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('plan-action-btn')).toBeDefined();
		});

		const actionBtn = screen.getByTestId('plan-action-btn');
		await actionBtn.click();

		await vi.waitFor(() => {
			expect(screen.getByTestId('plan-confirm-dialog')).toBeDefined();
			expect(screen.getByTestId('proration-notice')).toBeDefined();
		});

		expect(
			screen.getByText('Tidak ada proration: periode baru dimulai saat pembayaran diterima')
		).toBeDefined();

		const proceedBtn = screen.getByTestId('confirm-dialog-proceed');
		await proceedBtn.click();

		await vi.waitFor(() => {
			expect(onSelectPlan).toHaveBeenCalledWith(proPlan, 'checkout');
		});
	});
});
