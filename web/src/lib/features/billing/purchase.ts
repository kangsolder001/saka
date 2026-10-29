import { goto } from '$app/navigation';
import { toast } from 'svelte-sonner';
import { formatDate } from '$lib/format/date';
import { queryKeys } from '$lib/api/query-keys';
import type { QueryClient } from '@tanstack/svelte-query';
import type { CheckoutResponse, ChangePlanResponse, Invoice } from '$lib/api/types';
import { openFakePaymentDialog } from './fake-dialog.svelte';
import { openScheduledDowngradeDialog, type DowngradeWarning } from './downgrade-dialog.svelte';
import * as m from '$lib/paraglide/messages.js';

export interface PurchaseOptions {
	onScheduledDowngrade?: (info: {
		pendingPlan: string;
		currentPeriodEnd: string | null;
		warnings: DowngradeWarning[];
	}) => void;
	onFakePayment?: (invoice: Invoice) => void;
	navigate?: (url: string) => Promise<void> | void;
}

/**
 * Initiates payment redirect or dev fake payment dialog (§8.3, §8.6).
 */
export function goToPayment(
	params: { orgId: string; invoice: Invoice },
	options?: { onFakePayment?: (invoice: Invoice) => void }
): void {
	const { orgId, invoice } = params;
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.setItem('saka.pendingPayment', JSON.stringify({ orgId, invoiceId: invoice.id }));
	}

	if (!invoice.payment_url) return;

	let host = '';
	try {
		host = new URL(invoice.payment_url).host;
	} catch {
		// ignore
	}

	if (import.meta.env.DEV && host === 'pay.fake.test') {
		if (options?.onFakePayment) {
			options.onFakePayment(invoice);
		}
		openFakePaymentDialog({ orgId, invoice });
	} else {
		if (typeof window !== 'undefined') {
			window.location.assign(invoice.payment_url);
		}
	}
}

/**
 * Handles the result of checkout or plan change per §8.3.
 */
export async function handlePurchaseResult(
	result: CheckoutResponse | ChangePlanResponse,
	orgId: string,
	qc: QueryClient,
	options?: PurchaseOptions
): Promise<void> {
	// 1. trial_started === true
	if (result.trial_started) {
		const plan = result.subscription.plan;
		const trialEndsAt = result.subscription.trial_ends_at
			? formatDate(result.subscription.trial_ends_at)
			: '';
		toast.success(m.billing_trial_started_toast({ plan, trial_ends_at: trialEndsAt }));

		await Promise.all([
			qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) }),
			qc.invalidateQueries({ queryKey: queryKeys.invoices(orgId) })
		]);

		const targetUrl = `/orgs/${orgId}/billing`;
		if (options?.navigate) {
			await options.navigate(targetUrl);
		} else {
			await goto(targetUrl);
		}
		return;
	}

	// 2. scheduled === true
	if ('scheduled' in result && result.scheduled) {
		const pendingPlan = result.subscription.pending_plan || result.subscription.plan;
		const currentPeriodEnd = result.subscription.current_period_end;
		const warnings: DowngradeWarning[] = result.warnings || [];

		if (options?.onScheduledDowngrade) {
			options.onScheduledDowngrade({ pendingPlan, currentPeriodEnd, warnings });
		}
		openScheduledDowngradeDialog({ pendingPlan, currentPeriodEnd, warnings });

		await qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
		return;
	}

	// 3. cleared === true
	if ('cleared' in result && result.cleared) {
		toast.success(m.billing_change_cleared_toast());
		await qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
		return;
	}

	// 4. invoice with payment_url
	if (result.invoice?.payment_url) {
		goToPayment({ orgId, invoice: result.invoice }, options);
		return;
	}

	// 5. else
	await qc.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
}
