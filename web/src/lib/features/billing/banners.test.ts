import { describe, expect, it } from 'vitest';
import { subscriptionBanner } from './banners';
import type { Subscription } from '$lib/api/types';

describe('subscriptionBanner (§8.5)', () => {
	const baseSub: Subscription = {
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

	it('returns null if sub is null or undefined', () => {
		expect(subscriptionBanner(null)).toBeNull();
		expect(subscriptionBanner(undefined)).toBeNull();
	});

	it('returns null for standard active subscription with no pending changes', () => {
		expect(subscriptionBanner(baseSub)).toBeNull();
	});

	it('row 1: status = past_due returns warning banner with invoices action', () => {
		const sub: Subscription = {
			...baseSub,
			status: 'past_due',
			grace_until: '2026-02-10T00:00:00Z'
		};

		const result = subscriptionBanner(sub);
		expect(result).not.toBeNull();
		expect(result?.variant).toBe('warning');
		expect(result?.messageKey).toBe('billing_banner_past_due');
		expect(result?.params).toEqual({
			plan: 'pro',
			date: '2026-02-10T00:00:00Z'
		});
		expect(result?.action).toEqual({
			labelKey: 'billing_banner_action_pay_now',
			type: 'invoices',
			label: 'Bayar sekarang'
		});
		expect(result?.actionType).toBe('invoices');
		expect(result?.message).toContain('Pembayaran tertunda');
	});

	it('row 2: status = trialing and trial_ends_at <= 7 days returns info banner', () => {
		const now = new Date('2026-02-01T00:00:00Z');
		const sub: Subscription = {
			...baseSub,
			status: 'trialing',
			trial_ends_at: '2026-02-05T00:00:00Z' // 4 days remaining
		};

		const result = subscriptionBanner(sub, now);
		expect(result).not.toBeNull();
		expect(result?.variant).toBe('info');
		expect(result?.messageKey).toBe('billing_banner_trialing');
		expect(result?.params).toEqual({
			plan: 'pro',
			days: 4
		});
		expect(result?.action).toEqual({
			labelKey: 'billing_banner_action_pay_now',
			type: 'invoices',
			label: 'Bayar sekarang'
		});
		expect(result?.actionType).toBe('invoices');
		expect(result?.message).toContain('Trial pro berakhir dalam 4 hari');
	});

	it('row 2 edge: status = trialing but trial_ends_at > 7 days returns null', () => {
		const now = new Date('2026-02-01T00:00:00Z');
		const sub: Subscription = {
			...baseSub,
			status: 'trialing',
			trial_ends_at: '2026-02-15T00:00:00Z' // 14 days remaining
		};

		expect(subscriptionBanner(sub, now)).toBeNull();
	});

	it('row 3: cancel_at_period_end = true returns warning banner with resume action', () => {
		const sub: Subscription = {
			...baseSub,
			cancel_at_period_end: true,
			current_period_end: '2026-03-01T00:00:00Z'
		};

		const result = subscriptionBanner(sub);
		expect(result).not.toBeNull();
		expect(result?.variant).toBe('warning');
		expect(result?.messageKey).toBe('billing_banner_canceling');
		expect(result?.params).toEqual({
			date: '2026-03-01T00:00:00Z'
		});
		expect(result?.action).toEqual({
			labelKey: 'billing_banner_action_resume',
			type: 'resume',
			label: 'Lanjutkan langganan'
		});
		expect(result?.actionType).toBe('resume');
		expect(result?.message).toContain('Langganan berhenti pada');
	});

	it('row 4: pending_plan !== null returns info banner with cancel_pending action', () => {
		const sub: Subscription = {
			...baseSub,
			pending_plan: 'free',
			current_period_end: '2026-03-01T00:00:00Z'
		};

		const result = subscriptionBanner(sub);
		expect(result).not.toBeNull();
		expect(result?.variant).toBe('info');
		expect(result?.messageKey).toBe('billing_banner_downgrade_scheduled');
		expect(result?.params).toEqual({
			plan: 'free',
			date: '2026-03-01T00:00:00Z'
		});
		expect(result?.action).toEqual({
			labelKey: 'billing_banner_action_cancel_change',
			type: 'cancel_pending',
			label: 'Batalkan perubahan'
		});
		expect(result?.actionType).toBe('cancel_pending');
		expect(result?.message).toContain('Plan berganti ke free');
	});

	it('row 5: expired/canceled paid plan returns neutral banner with select plan action', () => {
		const expiredSub: Subscription = {
			...baseSub,
			status: 'expired',
			plan: 'pro'
		};

		const result = subscriptionBanner(expiredSub);
		expect(result).not.toBeNull();
		expect(result?.variant).toBe('neutral');
		expect(result?.messageKey).toBe('billing_banner_ended');
		expect(result?.params).toEqual({
			plan: 'pro'
		});
		expect(result?.action).toEqual({
			labelKey: 'billing_banner_action_select_plan',
			type: 'plans',
			label: 'Pilih plan'
		});
		expect(result?.actionType).toBe('plans');
		expect(result?.message).toContain('Langganan pro sudah berakhir; organisasi kembali ke Free');

		const canceledSub: Subscription = {
			...baseSub,
			status: 'canceled',
			plan: 'enterprise'
		};

		const canceledResult = subscriptionBanner(canceledSub);
		expect(canceledResult?.variant).toBe('neutral');
		expect(canceledResult?.messageKey).toBe('billing_banner_ended');
		expect(canceledResult?.actionType).toBe('plans');
	});

	it('row 5 edge: expired/canceled on free plan returns null', () => {
		const freeSub: Subscription = {
			...baseSub,
			plan: 'free',
			status: 'expired'
		};

		expect(subscriptionBanner(freeSub)).toBeNull();
	});

	it('priority: past_due takes precedence over cancel_at_period_end and pending_plan', () => {
		const sub: Subscription = {
			...baseSub,
			status: 'past_due',
			grace_until: '2026-02-10T00:00:00Z',
			cancel_at_period_end: true,
			pending_plan: 'free'
		};

		const result = subscriptionBanner(sub);
		expect(result?.variant).toBe('warning');
		expect(result?.messageKey).toBe('billing_banner_past_due');
		expect(result?.actionType).toBe('invoices');
		expect(result?.message).toContain('Pembayaran tertunda');
	});
});
