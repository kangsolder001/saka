import * as m from '$lib/paraglide/messages';
import { formatDate } from '$lib/format/date';
import type { Plan, Subscription } from '$lib/api/types';

export type PlanActionType = 'none' | 'checkout' | 'change-plan';

export interface PlanActionResult {
	action: PlanActionType;
	labelKey: string;
	label: string;
	params?: Record<string, string | number>;
}

/**
 * Pure function to resolve the action and button label for a plan card (§8.2).
 * Evaluates conditions strictly in the order defined in the spec table.
 */
export function resolvePlanAction(
	plan: Plan,
	subscription?: Subscription | null,
	currentPlanOrPlans?: Plan | Plan[] | null
): PlanActionResult {
	const isPaidPlan = plan.price_amount > 0;

	// 1. sub.status adalah expired atau canceled
	if (subscription && (subscription.status === 'expired' || subscription.status === 'canceled')) {
		if (isPaidPlan) {
			return {
				action: 'checkout',
				labelKey: 'billing_action_subscribe',
				label: m.billing_action_subscribe()
			};
		}
		return {
			action: 'none',
			labelKey: 'billing_action_current_plan',
			label: m.billing_action_current_plan()
		};
	}

	// 2. plan.code === sub.plan dan sub.status adalah trialing atau past_due
	if (
		subscription &&
		plan.code === subscription.plan &&
		(subscription.status === 'trialing' || subscription.status === 'past_due')
	) {
		return {
			action: 'change-plan',
			labelKey: 'billing_action_pay_now',
			label: m.billing_action_pay_now()
		};
	}

	// 3. plan.code === sub.plan dan sub.pending_plan !== null
	if (subscription && plan.code === subscription.plan && subscription.pending_plan != null) {
		return {
			action: 'change-plan',
			labelKey: 'billing_action_cancel_change',
			label: m.billing_action_cancel_change()
		};
	}

	// 4. plan.code === sub.plan
	if (subscription && plan.code === subscription.plan) {
		return {
			action: 'none',
			labelKey: 'billing_action_current_plan',
			label: m.billing_action_current_plan()
		};
	}

	// 5. sub.plan === 'free' (masih Free atau belum ada subscription)
	if (!subscription || subscription.plan === 'free') {
		const labelKey = plan.trial_days > 0 ? 'billing_action_try_free' : 'billing_action_select_plan';
		const params = { days: plan.trial_days };
		const label =
			plan.trial_days > 0 ? m.billing_action_try_free(params) : m.billing_action_select_plan();
		return {
			action: 'checkout',
			labelKey,
			label,
			params
		};
	}

	// 6. plan.code === sub.pending_plan
	if (subscription.pending_plan != null && plan.code === subscription.pending_plan) {
		const rawDate = subscription.current_period_end ?? '';
		const formattedDate = rawDate ? formatDate(rawDate) : '';
		return {
			action: 'none',
			labelKey: 'billing_action_scheduled',
			label: m.billing_action_scheduled({ date: formattedDate }),
			params: { date: rawDate }
		};
	}

	// 7. harga plan > harga plan sekarang (look up by sub.plan, or 0 if free/not found) -> upgrade
	let currentPrice = 0;
	if (currentPlanOrPlans) {
		if (Array.isArray(currentPlanOrPlans)) {
			const found = currentPlanOrPlans.find((p) => p.code === subscription.plan);
			if (found) currentPrice = found.price_amount;
		} else {
			currentPrice = currentPlanOrPlans.price_amount;
		}
	} else if (subscription.plan === 'free') {
		currentPrice = 0;
	} else if (subscription.plan === plan.code) {
		currentPrice = plan.price_amount;
	}

	if (plan.price_amount > currentPrice) {
		return {
			action: 'change-plan',
			labelKey: 'billing_action_upgrade',
			label: m.billing_action_upgrade()
		};
	}

	// 8. selain itu -> downgrade
	return {
		action: 'change-plan',
		labelKey: 'billing_action_downgrade',
		label: m.billing_action_downgrade()
	};
}
