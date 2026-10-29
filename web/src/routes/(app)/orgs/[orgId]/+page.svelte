<script lang="ts">
	import { page } from '$app/state';
	import { CreditCard, Users, ArrowRight, Layers, FileText } from 'lucide-svelte';
	import { createOrganizationQuery } from '$lib/features/org/api';
	import { createMeQuery } from '$lib/features/account/api';
	import { createSubscriptionQuery, createInvoicesQuery } from '$lib/features/billing/api';
	import { can } from '$lib/features/org/permissions';
	import { formatDate } from '$lib/format/date';
	import { formatMoney } from '$lib/format/money';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import RoleBadge from '$lib/components/app/RoleBadge.svelte';
	import StatusBadge from '$lib/components/app/StatusBadge.svelte';
	import EmailVerificationBanner from '$lib/components/app/EmailVerificationBanner.svelte';
	import EmptyState from '$lib/components/app/EmptyState.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';

	let orgId = $derived(page.params.orgId ?? '');

	const orgQuery = createOrganizationQuery(() => orgId);
	const meQuery = createMeQuery();
	const subQuery = createSubscriptionQuery(() => orgId);
	const invQuery = createInvoicesQuery(() => orgId);

	let org = $derived(orgQuery.data?.organization);
	let role = $derived(org?.role);
	let user = $derived(meQuery.data?.user);
	let subscription = $derived(subQuery.data);

	// First open invoice for owner
	let openInvoice = $derived.by(() => {
		if (!can(role, 'invoices.view')) return null;
		const items = invQuery.data?.items ?? [];
		return items.find((inv) => inv.status === 'open') ?? null;
	});

	let memberUsageText = $derived.by(() => {
		if (!subscription?.usage?.members) return null;
		const { used, max } = subscription.usage.members;
		if (max === -1) {
			return m.members_usage_unlimited({ used });
		}
		return m.members_usage({ used, max });
	});
</script>

{#if orgQuery.isPending || meQuery.isPending}
	<div class="p-6">
		<PageSkeleton />
	</div>
{:else if orgQuery.isError}
	<div class="p-6">
		<ErrorState error={orgQuery.error} retry={() => orgQuery.refetch()} />
	</div>
{:else if org}
	<div class="flex flex-col gap-6 p-4 sm:p-6 lg:p-8" data-testid="dashboard-summary">
		<!-- Email verification warning if not verified -->
		{#if user && !user.email_verified}
			<EmailVerificationBanner />
		{/if}

		<!-- Page Header with role badge -->
		<PageHeader title={org.name} description={org.slug ? `@${org.slug}` : undefined}>
			{#snippet actions()}
				{#if role}
					<RoleBadge {role} />
				{/if}
			{/snippet}
		</PageHeader>

		<!-- Cards Grid -->
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
			<!-- 1. Plan Card -->
			<div
				class="flex flex-col justify-between rounded-lg border border-border bg-card p-5"
				data-testid="dashboard-plan-card"
			>
				<div class="flex flex-col gap-3">
					<div class="flex items-center justify-between">
						<span class="text-xs font-medium tracking-wider text-muted-foreground uppercase">
							{m.dashboard_plan_card_title()}
						</span>
						<CreditCard class="size-4 text-muted-foreground" />
					</div>

					<div class="flex items-center gap-2">
						<span class="text-xl font-bold tracking-tight text-foreground capitalize">
							{subscription?.effective_plan ?? 'Free'}
						</span>
						{#if subscription?.status}
							<StatusBadge kind="subscription" status={subscription.status} />
						{/if}
					</div>

					{#if subscription?.trial_ends_at && subscription.status === 'trialing'}
						<p class="text-xs text-muted-foreground">
							Trial ends: {formatDate(subscription.trial_ends_at)}
						</p>
					{:else if subscription?.current_period_end}
						<p class="text-xs text-muted-foreground">
							Period ends: {formatDate(subscription.current_period_end)}
						</p>
					{/if}
				</div>

				<div class="mt-5 border-t border-border/50 pt-3">
					<Button
						variant="outline"
						size="sm"
						href={`/orgs/${orgId}/billing`}
						class="min-h-[44px] w-full justify-between text-xs"
					>
						<span>{m.dashboard_plan_card_link()}</span>
						<ArrowRight class="ml-1 size-3.5" />
					</Button>
				</div>
			</div>

			<!-- 2. Members Card -->
			<div
				class="flex flex-col justify-between rounded-lg border border-border bg-card p-5"
				data-testid="dashboard-members-card"
			>
				<div class="flex flex-col gap-3">
					<div class="flex items-center justify-between">
						<span class="text-xs font-medium tracking-wider text-muted-foreground uppercase">
							{m.dashboard_members_card_title()}
						</span>
						<Users class="size-4 text-muted-foreground" />
					</div>

					<div class="flex items-baseline gap-2">
						<span class="text-xl font-bold tracking-tight text-foreground">
							{memberUsageText ?? '—'}
						</span>
					</div>

					<p class="text-xs text-muted-foreground">
						{m.members_desc()}
					</p>
				</div>

				<div class="mt-5 flex items-center gap-2 border-t border-border/50 pt-3">
					<Button
						variant="outline"
						size="sm"
						href={`/orgs/${orgId}/members`}
						class="min-h-[44px] flex-1 justify-between text-xs"
					>
						<span>{m.nav_members()}</span>
						<ArrowRight class="ml-1 size-3.5" />
					</Button>

					{#if can(role, 'invitations.manage')}
						<Button
							variant="default"
							size="sm"
							href={`/orgs/${orgId}/members`}
							class="min-h-[44px] text-xs"
						>
							{m.dashboard_members_card_invite()}
						</Button>
					{/if}
				</div>
			</div>

			<!-- 3. Unpaid Invoice Card (Owner only, if open invoice exists) -->
			{#if openInvoice}
				<div
					class="flex flex-col justify-between rounded-lg border border-amber-500/30 bg-amber-500/5 p-5"
					data-testid="dashboard-unpaid-invoice-card"
				>
					<div class="flex flex-col gap-3">
						<div class="flex items-center justify-between">
							<span
								class="text-xs font-semibold tracking-wider text-amber-700 uppercase dark:text-amber-400"
							>
								{m.dashboard_unpaid_invoice_title()}
							</span>
							<FileText class="size-4 text-amber-600 dark:text-amber-400" />
						</div>

						<div>
							<div class="text-xl font-bold text-foreground">
								{formatMoney(openInvoice.amount, openInvoice.currency)}
							</div>
							<p class="mt-0.5 text-xs text-muted-foreground">
								Invoice #{openInvoice.number ?? openInvoice.id.slice(0, 8)}
							</p>
						</div>

						{#if openInvoice.due_at}
							<p class="text-xs text-amber-800 dark:text-amber-300">
								Due: {formatDate(openInvoice.due_at)}
							</p>
						{/if}
					</div>

					<div class="mt-5 border-t border-amber-500/20 pt-3">
						<Button
							variant="default"
							size="sm"
							href={`/orgs/${orgId}/billing`}
							class="min-h-[44px] w-full bg-amber-600 text-xs text-white hover:bg-amber-700"
						>
							{m.dashboard_unpaid_invoice_pay()}
						</Button>
					</div>
				</div>
			{/if}
		</div>

		<!-- 5. Domain Module Area: EmptyState Placeholder -->
		<div class="mt-4">
			<EmptyState
				title={m.dashboard_modules_empty_title()}
				description={m.dashboard_modules_empty_desc()}
				icon={Layers}
			/>
		</div>
	</div>
{/if}
