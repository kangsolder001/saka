import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import SubscriptionBanner from './SubscriptionBanner.svelte';
import type { Subscription, ChangePlanResponse } from '$lib/api/types';
import { resumeSubscription, changePlan } from '../api';
import * as navigation from '$app/navigation';
import { toast } from 'svelte-sonner';
import { setLocale } from '$lib/i18n';

vi.mock('$app/navigation', () => ({
	goto: vi.fn().mockResolvedValue(undefined)
}));

vi.mock('../api', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../api')>();
	return {
		...actual,
		resumeSubscription: vi.fn(),
		changePlan: vi.fn()
	};
});

vi.mock('svelte-sonner', () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn()
	}
}));

describe('SubscriptionBanner.svelte', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		setLocale('id');
	});

	const activeSub: Subscription = {
		plan: 'pro',
		status: 'active',
		trial_ends_at: null,
		current_period_start: '2026-01-01T00:00:00Z',
		current_period_end: '2026-02-01T00:00:00Z',
		cancel_at_period_end: false,
		pending_plan: null,
		grace_until: null,
		effective_plan: 'pro',
		usage: {}
	};

	it('renders nothing when there is no active banner condition', async () => {
		render(SubscriptionBanner, {
			orgId: 'org-123',
			role: 'owner',
			subscription: activeSub
		});

		expect(document.querySelector('[data-testid="subscription-banner-wrapper"]')).toBeNull();
	});

	it('renders warning banner with action for owner when status is past_due', async () => {
		const sub: Subscription = {
			...activeSub,
			status: 'past_due',
			grace_until: '2026-02-10T00:00:00Z'
		};

		const screen = render(SubscriptionBanner, {
			orgId: 'org-123',
			role: 'owner',
			subscription: sub
		});

		const message = screen.getByTestId('subscription-banner-message');
		expect(message.element().textContent).toContain('Pembayaran tertunda');

		const actionBtn = screen.getByTestId('subscription-banner-action');
		expect(actionBtn.element().textContent).toContain('Bayar sekarang');

		await actionBtn.click();
		expect(navigation.goto).toHaveBeenCalledWith('/orgs/org-123/billing/invoices');
	});

	it('renders message but NO action button for non-owner (member or admin)', async () => {
		const sub: Subscription = {
			...activeSub,
			status: 'past_due',
			grace_until: '2026-02-10T00:00:00Z'
		};

		const screen = render(SubscriptionBanner, {
			orgId: 'org-123',
			role: 'member',
			subscription: sub
		});

		const message = screen.getByTestId('subscription-banner-message');
		expect(message.element().textContent).toContain('Pembayaran tertunda');
		expect(document.querySelector('[data-testid="subscription-banner-action"]')).toBeNull();
	});

	it('renders resume action for owner when cancel_at_period_end is true and calls resumeSubscription', async () => {
		vi.mocked(resumeSubscription).mockResolvedValue({
			...activeSub,
			cancel_at_period_end: false
		});

		const sub: Subscription = {
			...activeSub,
			cancel_at_period_end: true,
			current_period_end: '2026-03-01T00:00:00Z'
		};

		const screen = render(SubscriptionBanner, {
			orgId: 'org-123',
			role: 'owner',
			subscription: sub
		});

		const actionBtn = screen.getByTestId('subscription-banner-action');
		expect(actionBtn.element().textContent).toContain('Lanjutkan langganan');

		await actionBtn.click();
		expect(resumeSubscription).toHaveBeenCalledWith('org-123');
		expect(toast.success).toHaveBeenCalled();
	});

	it('renders cancel downgrade action for owner when pending_plan exists', async () => {
		vi.mocked(changePlan).mockResolvedValue({
			cleared: true
		} as unknown as ChangePlanResponse);

		const sub: Subscription = {
			...activeSub,
			pending_plan: 'free',
			current_period_end: '2026-03-01T00:00:00Z'
		};

		const screen = render(SubscriptionBanner, {
			orgId: 'org-123',
			role: 'owner',
			subscription: sub
		});

		const actionBtn = screen.getByTestId('subscription-banner-action');
		expect(actionBtn.element().textContent).toContain('Batalkan perubahan');

		await actionBtn.click();
		expect(changePlan).toHaveBeenCalledWith('org-123', { plan: 'pro' });
		expect(toast.success).toHaveBeenCalled();
	});

	it('renders plans navigation when plan has ended/expired', async () => {
		const sub: Subscription = {
			...activeSub,
			status: 'expired',
			plan: 'pro'
		};

		const screen = render(SubscriptionBanner, {
			orgId: 'org-123',
			role: 'owner',
			subscription: sub
		});

		const actionBtn = screen.getByTestId('subscription-banner-action');
		expect(actionBtn.element().textContent).toContain('Pilih plan');

		await actionBtn.click();
		expect(navigation.goto).toHaveBeenCalledWith('/orgs/org-123/billing/plans');
	});
});
