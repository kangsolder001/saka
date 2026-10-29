import { describe, expect, it } from 'vitest';
import { resolvePlanAction } from './plan-actions';
import type { Plan, Subscription } from '$lib/api/types';

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

const proPlan: Plan = {
	code: 'pro',
	name: 'Pro',
	price_amount: 99000,
	currency: 'IDR',
	interval: 'month',
	trial_days: 14,
	features: ['export_csv'],
	limits: { members: 10 }
};

const enterprisePlan: Plan = {
	code: 'enterprise',
	name: 'Enterprise',
	price_amount: 299000,
	currency: 'IDR',
	interval: 'month',
	trial_days: 0,
	features: ['export_csv', 'audit_log'],
	limits: { members: -1 }
};

const allPlans = [freePlan, proPlan, enterprisePlan];

function createSub(overrides: Partial<Subscription> = {}): Subscription {
	return {
		plan: 'free',
		status: 'active',
		trial_ends_at: null,
		current_period_start: '2026-01-01T00:00:00Z',
		current_period_end: '2026-02-01T00:00:00Z',
		cancel_at_period_end: false,
		pending_plan: null,
		grace_until: null,
		effective_plan: 'free',
		usage: { members: { used: 1, max: 3 } },
		...overrides
	};
}

describe('resolvePlanAction (§8.2)', () => {
	it('Row 1: sub.status expired or canceled -> paid plan = checkout ("Berlangganan"), Free = none ("Plan saat ini")', () => {
		const expiredSub = createSub({ plan: 'pro', status: 'expired' });
		const expPaid = resolvePlanAction(proPlan, expiredSub, allPlans);
		expect(expPaid.action).toBe('checkout');
		expect(expPaid.labelKey).toBe('billing_action_subscribe');
		expect(expPaid.label).toBe('Berlangganan');

		const expFree = resolvePlanAction(freePlan, expiredSub, allPlans);
		expect(expFree.action).toBe('none');
		expect(expFree.labelKey).toBe('billing_action_current_plan');
		expect(expFree.label).toBe('Plan saat ini');

		const canceledSub = createSub({ plan: 'pro', status: 'canceled' });
		const canPaid = resolvePlanAction(enterprisePlan, canceledSub, allPlans);
		expect(canPaid.action).toBe('checkout');
		expect(canPaid.labelKey).toBe('billing_action_subscribe');

		const canFree = resolvePlanAction(freePlan, canceledSub, allPlans);
		expect(canFree.action).toBe('none');
		expect(canFree.labelKey).toBe('billing_action_current_plan');
	});

	it('Row 2: plan.code === sub.plan and status is trialing or past_due -> change-plan ("Bayar sekarang")', () => {
		const trialingSub = createSub({ plan: 'pro', status: 'trialing' });
		const resTrial = resolvePlanAction(proPlan, trialingSub, allPlans);
		expect(resTrial.action).toBe('change-plan');
		expect(resTrial.labelKey).toBe('billing_action_pay_now');
		expect(resTrial.label).toBe('Bayar sekarang');

		const pastDueSub = createSub({ plan: 'pro', status: 'past_due' });
		const resPastDue = resolvePlanAction(proPlan, pastDueSub, allPlans);
		expect(resPastDue.action).toBe('change-plan');
		expect(resPastDue.labelKey).toBe('billing_action_pay_now');
	});

	it('Row 3: plan.code === sub.plan and pending_plan !== null -> change-plan ("Batalkan perubahan")', () => {
		const subWithPending = createSub({
			plan: 'enterprise',
			status: 'active',
			pending_plan: 'pro'
		});
		const res = resolvePlanAction(enterprisePlan, subWithPending, allPlans);
		expect(res.action).toBe('change-plan');
		expect(res.labelKey).toBe('billing_action_cancel_change');
		expect(res.label).toBe('Batalkan perubahan');
	});

	it('Row 4: plan.code === sub.plan -> none ("Plan saat ini")', () => {
		const activeProSub = createSub({ plan: 'pro', status: 'active', pending_plan: null });
		const res = resolvePlanAction(proPlan, activeProSub, allPlans);
		expect(res.action).toBe('none');
		expect(res.labelKey).toBe('billing_action_current_plan');
		expect(res.label).toBe('Plan saat ini');
	});

	it('Row 5: sub.plan === "free" or null -> checkout ("Coba gratis {days} hari" or "Pilih plan")', () => {
		const freeSub = createSub({ plan: 'free', status: 'active' });
		// Pro has trial_days: 14
		const resPro = resolvePlanAction(proPlan, freeSub, allPlans);
		expect(resPro.action).toBe('checkout');
		expect(resPro.labelKey).toBe('billing_action_try_free');
		expect(resPro.params).toEqual({ days: 14 });
		expect(resPro.label).toBe('Coba gratis 14 hari');

		// Enterprise has trial_days: 0
		const resEnt = resolvePlanAction(enterprisePlan, freeSub, allPlans);
		expect(resEnt.action).toBe('checkout');
		expect(resEnt.labelKey).toBe('billing_action_select_plan');
		expect(resEnt.label).toBe('Pilih plan');

		// Null subscription defaults to Free behavior
		const resNull = resolvePlanAction(proPlan, null, allPlans);
		expect(resNull.action).toBe('checkout');
		expect(resNull.labelKey).toBe('billing_action_try_free');
	});

	it('Row 6: plan.code === sub.pending_plan -> none ("Terjadwal mulai {date}")', () => {
		const subDowngrading = createSub({
			plan: 'enterprise',
			status: 'active',
			pending_plan: 'pro',
			current_period_end: '2026-03-01T00:00:00Z'
		});
		const result = resolvePlanAction(proPlan, subDowngrading, allPlans);
		expect(result.action).toBe('none');
		expect(result.labelKey).toBe('billing_action_scheduled');
		expect(result.params).toEqual({ date: '2026-03-01T00:00:00Z' });
		expect(result.label).toContain('Terjadwal');
	});

	it('Row 7: price > currentPrice -> change-plan ("Upgrade")', () => {
		const activeProSub = createSub({ plan: 'pro', status: 'active' });
		const res = resolvePlanAction(enterprisePlan, activeProSub, allPlans);
		expect(res.action).toBe('change-plan');
		expect(res.labelKey).toBe('billing_action_upgrade');
		expect(res.label).toBe('Upgrade');
	});

	it('Row 8: price <= currentPrice -> change-plan ("Downgrade")', () => {
		const activeEnterpriseSub = createSub({ plan: 'enterprise', status: 'active' });
		const res = resolvePlanAction(proPlan, activeEnterpriseSub, allPlans);
		expect(res.action).toBe('change-plan');
		expect(res.labelKey).toBe('billing_action_downgrade');
		expect(res.label).toBe('Downgrade');
	});
});
