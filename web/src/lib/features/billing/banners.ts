import type { Subscription } from '$lib/api/types';
import { formatDate } from '$lib/format/date';
import * as m from '$lib/paraglide/messages';

export type BannerActionType = 'invoices' | 'plans' | 'resume' | 'cancel_pending';

export interface BannerAction {
	labelKey: string;
	type: BannerActionType;
	label: string;
}

export interface BannerConfig {
	variant: 'warning' | 'info' | 'neutral';
	messageKey: string;
	params?: Record<string, string | number>;
	action: BannerAction;

	// Backward-compatibility properties with SubscriptionBannerResult:
	message: string;
	actionType: BannerActionType;
	actionLabel: string;
}

export type SubscriptionBannerResult = BannerConfig;

/**
 * Pure function to determine active subscription banner based on spec §8.5.
 * Checked strictly in priority order.
 */
export function subscriptionBanner(
	sub: Subscription | null | undefined,
	now: Date = new Date()
): BannerConfig | null {
	if (!sub) return null;

	// 1. sub.status === 'past_due'
	if (sub.status === 'past_due') {
		const rawDate = sub.grace_until ?? '';
		const formattedDate = rawDate ? formatDate(rawDate) : '';
		const params = {
			plan: sub.plan,
			date: rawDate
		};
		const action: BannerAction = {
			labelKey: 'billing_banner_action_pay_now',
			type: 'invoices',
			label: m.billing_banner_action_pay_now()
		};
		return {
			variant: 'warning',
			messageKey: 'billing_banner_past_due',
			params,
			action,
			message: m.billing_banner_past_due({ plan: sub.plan, date: formattedDate }),
			actionType: action.type,
			actionLabel: action.label
		};
	}

	// 2. sub.status === 'trialing' and trial_ends_at is <= 7 days from now
	if (sub.status === 'trialing' && sub.trial_ends_at) {
		const trialEnd = new Date(sub.trial_ends_at).getTime();
		const diffMs = trialEnd - now.getTime();
		const sevenDaysMs = 7 * 24 * 60 * 60 * 1000;

		if (diffMs <= sevenDaysMs) {
			const diffDays = Math.max(0, Math.ceil(diffMs / (24 * 60 * 60 * 1000)));
			const params = {
				plan: sub.plan,
				days: diffDays
			};
			const action: BannerAction = {
				labelKey: 'billing_banner_action_pay_now',
				type: 'invoices',
				label: m.billing_banner_action_pay_now()
			};
			return {
				variant: 'info',
				messageKey: 'billing_banner_trialing',
				params,
				action,
				message: m.billing_banner_trialing({ plan: sub.plan, days: diffDays }),
				actionType: action.type,
				actionLabel: action.label
			};
		}
	}

	// 3. sub.cancel_at_period_end === true
	if (sub.cancel_at_period_end === true) {
		const rawDate = sub.current_period_end ?? '';
		const formattedDate = rawDate ? formatDate(rawDate) : '';
		const params = {
			date: rawDate
		};
		const action: BannerAction = {
			labelKey: 'billing_banner_action_resume',
			type: 'resume',
			label: m.billing_banner_action_resume()
		};
		return {
			variant: 'warning',
			messageKey: 'billing_banner_canceling',
			params,
			action,
			message: m.billing_banner_canceling({ date: formattedDate }),
			actionType: action.type,
			actionLabel: action.label
		};
	}

	// 4. sub.pending_plan !== null
	if (sub.pending_plan !== null && sub.pending_plan !== undefined) {
		const rawDate = sub.current_period_end ?? '';
		const formattedDate = rawDate ? formatDate(rawDate) : '';
		const params = {
			plan: sub.pending_plan,
			date: rawDate
		};
		const action: BannerAction = {
			labelKey: 'billing_banner_action_cancel_change',
			type: 'cancel_pending',
			label: m.billing_banner_action_cancel_change()
		};
		return {
			variant: 'info',
			messageKey: 'billing_banner_downgrade_scheduled',
			params,
			action,
			message: m.billing_banner_downgrade_scheduled({
				plan: sub.pending_plan,
				date: formattedDate
			}),
			actionType: action.type,
			actionLabel: action.label
		};
	}

	// 5. (sub.status === 'expired' || sub.status === 'canceled') && sub.plan !== 'free'
	if ((sub.status === 'expired' || sub.status === 'canceled') && sub.plan !== 'free') {
		const params = {
			plan: sub.plan
		};
		const action: BannerAction = {
			labelKey: 'billing_banner_action_select_plan',
			type: 'plans',
			label: m.billing_banner_action_select_plan()
		};
		return {
			variant: 'neutral',
			messageKey: 'billing_banner_ended',
			params,
			action,
			message: m.billing_banner_ended({ plan: sub.plan }),
			actionType: action.type,
			actionLabel: action.label
		};
	}

	// 6. else: null
	return null;
}
