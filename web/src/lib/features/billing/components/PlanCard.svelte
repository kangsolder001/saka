<script lang="ts">
	import { Check } from 'lucide-svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { formatMoney } from '$lib/format/money';
	import { can } from '$lib/features/org/permissions';
	import { resolvePlanAction } from '../plan-actions';
	import type { Plan, Subscription, Role } from '$lib/api/types';
	import * as m from '$lib/paraglide/messages.js';

	interface Props {
		plan: Plan;
		subscription: Subscription;
		role?: Role | null;
		allPlans?: Plan[];
		loading?: boolean;
		onSelectPlan?: (plan: Plan, action: 'checkout' | 'change-plan') => void | Promise<void>;
	}

	let {
		plan,
		subscription,
		role = 'owner',
		allPlans,
		loading = false,
		onSelectPlan
	}: Props = $props();

	let showConfirmDialog = $state(false);
	let confirming = $state(false);

	let isOwner = $derived(can(role, 'billing.manage'));
	let actionResult = $derived(resolvePlanAction(plan, subscription, allPlans));
	let isCurrentPlan = $derived(plan.code === subscription.plan);

	let intervalLabel = $derived(
		plan.interval === 'month' ? m.billing_per_month() : m.billing_per_year()
	);
	let formattedPrice = $derived(
		`${formatMoney(plan.price_amount, plan.currency)} ${intervalLabel}`
	);

	function getFeatureName(key: string): string {
		const messageKey = `billing_feature_${key}` as keyof typeof m;
		if (typeof m[messageKey] === 'function') {
			return (m[messageKey] as () => string)();
		}
		return key;
	}

	function getLimitName(key: string): string {
		const messageKey = `billing_limit_${key}` as keyof typeof m;
		if (typeof m[messageKey] === 'function') {
			return (m[messageKey] as () => string)();
		}
		return key;
	}

	function handleActionClick() {
		if (!isOwner || actionResult.action === 'none') return;
		showConfirmDialog = true;
	}

	async function handleConfirm() {
		if (actionResult.action === 'none') return;
		confirming = true;
		try {
			await onSelectPlan?.(plan, actionResult.action);
			showConfirmDialog = false;
		} finally {
			confirming = false;
		}
	}
</script>

<div
	class="flex flex-col justify-between rounded-lg border border-border bg-card p-6 text-card-foreground shadow-none"
	data-testid={`plan-card-${plan.code}`}
>
	<div class="space-y-6">
		<!-- Header -->
		<div class="flex items-start justify-between gap-2">
			<div>
				<h3 class="text-xl font-bold tracking-tight text-foreground" data-testid="plan-name">
					{plan.name}
				</h3>
				<div class="mt-2 flex items-baseline gap-1">
					<span class="text-2xl font-bold tracking-tight text-foreground" data-testid="plan-price">
						{formattedPrice}
					</span>
				</div>
			</div>
			{#if isCurrentPlan}
				<Badge variant="outline" class="font-medium" data-testid="current-plan-badge">
					{m.billing_plans_current_badge()}
				</Badge>
			{/if}
		</div>

		<!-- Features list -->
		{#if plan.features && plan.features.length > 0}
			<div class="space-y-2">
				<p class="text-xs font-semibold tracking-wider text-muted-foreground uppercase">
					{m.billing_features_heading()}
				</p>
				<ul class="space-y-2 text-sm text-foreground" data-testid="plan-features">
					{#each plan.features as feat (feat)}
						<li class="flex items-center gap-2">
							<Check class="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
							<span>{getFeatureName(feat)}</span>
						</li>
					{/each}
				</ul>
			</div>
		{/if}

		<!-- Limits list -->
		{#if plan.limits && Object.keys(plan.limits).length > 0}
			<div class="space-y-2 border-t border-border pt-4">
				<p class="text-xs font-semibold tracking-wider text-muted-foreground uppercase">
					{m.billing_limits_heading()}
				</p>
				<ul class="space-y-1.5 text-xs text-muted-foreground" data-testid="plan-limits">
					{#each Object.entries(plan.limits) as [limitKey, limitVal] (limitKey)}
						<li class="flex items-center justify-between">
							<span>{getLimitName(limitKey)}</span>
							<span class="font-medium text-foreground">
								{limitVal === -1 ? m.billing_unlimited() : limitVal}
							</span>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</div>

	<!-- Action footer -->
	<div class="mt-6 space-y-2 pt-4">
		{#if !isOwner}
			<p class="text-center text-xs text-muted-foreground" data-testid="only-owner-note">
				{m.billing_only_owner_note()}
			</p>
			<Button variant="outline" class="min-h-[44px] w-full" disabled data-testid="plan-action-btn">
				{actionResult.label}
			</Button>
		{:else if actionResult.action === 'none'}
			<Button variant="outline" class="min-h-[44px] w-full" disabled data-testid="plan-action-btn">
				{actionResult.label}
			</Button>
		{:else}
			<Button
				variant={plan.code === 'pro' ? 'default' : 'outline'}
				class="min-h-[44px] w-full"
				disabled={loading || confirming}
				onclick={handleActionClick}
				data-testid="plan-action-btn"
			>
				{actionResult.label}
			</Button>
		{/if}
	</div>
</div>

<!-- Confirmation Dialog before Checkout / Change Plan -->
<AlertDialog.Root open={showConfirmDialog} onOpenChange={(open) => (showConfirmDialog = open)}>
	<AlertDialog.Content
		class="border border-border shadow-none sm:max-w-[480px]"
		data-testid="plan-confirm-dialog"
	>
		<AlertDialog.Header>
			<AlertDialog.Title>{m.billing_confirm_title()}</AlertDialog.Title>
			<AlertDialog.Description>
				{m.billing_confirm_desc({
					plan: plan.name,
					price: formattedPrice
				})}
			</AlertDialog.Description>
		</AlertDialog.Header>

		<div
			class="rounded-md border border-border bg-muted/50 p-3 text-xs text-muted-foreground"
			data-testid="proration-notice"
		>
			{m.billing_confirm_no_proration_notice()}
		</div>

		<AlertDialog.Footer class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
			<Button
				type="button"
				variant="outline"
				class="min-h-[44px]"
				disabled={confirming}
				onclick={() => (showConfirmDialog = false)}
				data-testid="confirm-dialog-cancel"
			>
				{m.common_cancel()}
			</Button>
			<Button
				type="button"
				variant="default"
				class="min-h-[44px]"
				disabled={confirming}
				onclick={handleConfirm}
				data-testid="confirm-dialog-proceed"
			>
				{confirming
					? m.common_loading
						? m.common_loading()
						: 'Memuat...'
					: m.billing_confirm_proceed()}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
