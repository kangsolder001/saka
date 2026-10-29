<script lang="ts">
	import { page } from '$app/state';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import {
		createPlansQuery,
		createSubscriptionQuery,
		checkout,
		changePlan
	} from '$lib/features/billing/api';
	import { createOrganizationQuery } from '$lib/features/org/api';
	import { handlePurchaseResult } from '$lib/features/billing/purchase';
	import PlanCard from '$lib/features/billing/components/PlanCard.svelte';
	import DowngradeWarningDialog from '$lib/features/billing/components/DowngradeWarningDialog.svelte';
	import FakePaymentDialog from '$lib/features/billing/components/FakePaymentDialog.svelte';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import { errorMessage } from '$lib/api/error-message';
	import type { Plan } from '$lib/api/types';
	import * as m from '$lib/paraglide/messages.js';

	const queryClient = useQueryClient();

	let orgId = $derived(page.params.orgId ?? '');
	let plansQuery = createPlansQuery();
	let subQuery = createSubscriptionQuery(() => orgId);
	let orgQuery = createOrganizationQuery(() => orgId);

	let userRole = $derived(orgQuery.data?.organization?.role ?? 'member');

	let submitting = $state(false);

	async function handleSelectPlan(plan: Plan, action: 'checkout' | 'change-plan') {
		if (submitting) return;
		submitting = true;

		try {
			const res =
				action === 'checkout'
					? await checkout(orgId, plan.code)
					: await changePlan(orgId, plan.code);
			await handlePurchaseResult(res, orgId, queryClient);
		} catch (err: unknown) {
			toast.error(errorMessage(err));
		} finally {
			submitting = false;
		}
	}
</script>

<div class="space-y-6" data-testid="plans-page">
	<PageHeader title={m.billing_plans_title()} description={m.billing_plans_desc()} />

	{#if plansQuery.isPending || subQuery.isPending || orgQuery.isPending}
		<PageSkeleton />
	{:else if plansQuery.isError || subQuery.isError || orgQuery.isError}
		<ErrorState
			error={plansQuery.error || subQuery.error || orgQuery.error}
			retry={() => {
				plansQuery.refetch();
				subQuery.refetch();
				orgQuery.refetch();
			}}
		/>
	{:else if plansQuery.data && subQuery.data}
		<div class="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3" data-testid="plans-grid">
			{#each plansQuery.data as plan (plan.code)}
				<PlanCard
					{plan}
					subscription={subQuery.data}
					role={userRole}
					allPlans={plansQuery.data}
					loading={submitting}
					onSelectPlan={handleSelectPlan}
				/>
			{/each}
		</div>
	{/if}

	<DowngradeWarningDialog />
	<FakePaymentDialog {orgId} />
</div>
