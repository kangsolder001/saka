<script lang="ts">
	import { page } from '$app/state';
	import {
		createSubscriptionQuery,
		cancelSubscription,
		resumeSubscription
	} from '$lib/features/billing/api';
	import { createOrganizationQuery } from '$lib/features/org/api';
	import { can } from '$lib/features/org/permissions';
	import { formatDate } from '$lib/format/date';
	import StatusBadge from '$lib/components/app/StatusBadge.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import UsageMeter from '$lib/features/billing/components/UsageMeter.svelte';
	import { Button } from '$lib/components/ui/button';
	import { toast } from 'svelte-sonner';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import * as m from '$lib/paraglide/messages';

	let orgId = $derived(page.params.orgId ?? '');

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	let orgQuery = createOrganizationQuery(() => orgId);
	let subQuery = createSubscriptionQuery(() => orgId);

	let role = $derived(orgQuery.data?.organization?.role);
	let isOwner = $derived(can(role, 'billing.manage'));
	let sub = $derived(subQuery.data);

	let showCancelDialog = $state(false);
	let isCanceling = $state(false);
	let isResuming = $state(false);

	function getLimitLabel(key: string): string {
		if (key === 'members' && m.billing_limit_members) {
			return m.billing_limit_members();
		}
		const msgFn = (m as unknown as Record<string, (() => string) | undefined>)[
			`billing_limit_${key}`
		];
		if (typeof msgFn === 'function') {
			return msgFn();
		}
		return key.charAt(0).toUpperCase() + key.slice(1).replace(/_/g, ' ');
	}

	async function handleCancelSubscription() {
		try {
			isCanceling = true;
			await cancelSubscription(orgId);
			qc?.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
			toast.success(
				m.billing_cancel_success ? m.billing_cancel_success() : 'Langganan berhasil dibatalkan'
			);
			showCancelDialog = false;
		} catch {
			toast.error(
				m.error_internal_error ? m.error_internal_error() : 'Terjadi kesalahan pada server.'
			);
		} finally {
			isCanceling = false;
		}
	}

	async function handleResumeSubscription() {
		try {
			isResuming = true;
			await resumeSubscription(orgId);
			qc?.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
			toast.success(
				m.billing_resume_success ? m.billing_resume_success() : 'Langganan berhasil dilanjutkan'
			);
		} catch {
			toast.error(
				m.error_internal_error ? m.error_internal_error() : 'Terjadi kesalahan pada server.'
			);
		} finally {
			isResuming = false;
		}
	}
</script>

<div class="flex flex-col gap-6" data-testid="billing-overview-page">
	{#if subQuery.isPending}
		<div class="p-6">
			<PageSkeleton />
		</div>
	{:else if subQuery.isError}
		<div class="rounded-lg border border-border bg-card p-6">
			<ErrorState
				error={subQuery.error}
				title={m.common_warning ? m.common_warning() : 'Gagal memuat billing'}
				description={m.error_internal_error
					? m.error_internal_error()
					: 'Terjadi kesalahan saat memuat data langganan.'}
				retry={() => subQuery.refetch()}
			/>
		</div>
	{:else if sub}
		<!-- Plan Card -->
		<div
			class="flex flex-col gap-5 rounded-lg border border-border bg-card p-6 text-card-foreground"
			data-testid="billing-plan-card"
		>
			<div class="flex flex-wrap items-center justify-between gap-4">
				<div class="flex items-center gap-3">
					<h2
						class="text-xl font-bold tracking-tight text-foreground uppercase"
						data-testid="billing-plan-name"
					>
						{sub.effective_plan ?? sub.plan}
					</h2>
					<StatusBadge kind="subscription" status={sub.status} />
				</div>
			</div>

			<div class="flex flex-col gap-1.5">
				{#if sub.current_period_start && sub.current_period_end}
					<p class="text-sm text-muted-foreground" data-testid="billing-period">
						{m.billing_current_period
							? m.billing_current_period({
									start: formatDate(sub.current_period_start),
									end: formatDate(sub.current_period_end)
								})
							: `Periode berjalan: ${formatDate(sub.current_period_start)} – ${formatDate(sub.current_period_end)}`}
					</p>
				{/if}

				{#if sub.trial_ends_at}
					<p class="text-sm text-muted-foreground" data-testid="billing-trial-info">
						{m.billing_trial_ends_at({ date: formatDate(sub.trial_ends_at) })}
					</p>
				{/if}

				{#if sub.grace_until}
					<p class="text-sm font-medium text-warning" data-testid="billing-grace-info">
						{m.billing_grace_until({ date: formatDate(sub.grace_until) })}
					</p>
				{/if}

				{#if sub.pending_plan}
					<p class="text-sm font-medium text-info" data-testid="billing-pending-downgrade-info">
						{m.billing_banner_downgrade_scheduled({
							plan: sub.pending_plan,
							date: formatDate(sub.current_period_end)
						})}
					</p>
				{/if}
			</div>

			{#if isOwner}
				<div class="flex flex-wrap items-center gap-3 border-t border-border pt-4">
					<Button
						variant="default"
						class="min-h-[44px]"
						href={`/orgs/${orgId}/billing/plans`}
						data-testid="billing-change-plan-btn"
					>
						{m.billing_btn_change_plan ? m.billing_btn_change_plan() : 'Ubah plan'}
					</Button>

					{#if sub.plan !== 'free' && !sub.cancel_at_period_end}
						<Button
							variant="outline"
							class="min-h-[44px] text-destructive hover:bg-destructive/10"
							disabled={isCanceling}
							onclick={() => (showCancelDialog = true)}
							data-testid="billing-cancel-btn"
						>
							{m.billing_btn_cancel_subscription
								? m.billing_btn_cancel_subscription()
								: 'Batalkan langganan'}
						</Button>
					{/if}

					{#if sub.cancel_at_period_end}
						<Button
							variant="outline"
							class="min-h-[44px]"
							disabled={isResuming}
							onclick={handleResumeSubscription}
							data-testid="billing-resume-btn"
						>
							{m.billing_btn_resume_subscription
								? m.billing_btn_resume_subscription()
								: 'Lanjutkan langganan'}
						</Button>
					{/if}
				</div>
			{/if}
		</div>

		<!-- Usage Card -->
		{#if sub.usage && Object.keys(sub.usage).length > 0}
			<div
				class="flex flex-col gap-4 rounded-lg border border-border bg-card p-6 text-card-foreground"
				data-testid="billing-usage-card"
			>
				<h2 class="text-base font-semibold tracking-tight text-foreground">
					{m.billing_usage_title ? m.billing_usage_title() : 'Pemakaian'}
				</h2>
				<div class="flex flex-col gap-4">
					{#each Object.entries(sub.usage) as [key, limit] (key)}
						<UsageMeter
							label={getLimitLabel(key)}
							used={limit.used}
							max={limit.max}
							unit={key === 'members'
								? m.billing_unit_members
									? m.billing_unit_members()
									: 'anggota'
								: undefined}
						/>
					{/each}
				</div>
			</div>
		{/if}
	{/if}
</div>

{#if sub}
	<ConfirmDialog
		bind:open={showCancelDialog}
		title={m.billing_cancel_dialog_title ? m.billing_cancel_dialog_title() : 'Batalkan Langganan'}
		description={m.billing_cancel_dialog_desc
			? m.billing_cancel_dialog_desc({
					current_period_end: formatDate(sub.current_period_end)
				})
			: `Akses tetap berlaku sampai ${formatDate(sub.current_period_end)}, tanpa refund.`}
		variant="destructive"
		confirmLabel={m.billing_cancel_dialog_confirm
			? m.billing_cancel_dialog_confirm()
			: 'Ya, batalkan'}
		onConfirm={handleCancelSubscription}
	/>
{/if}
