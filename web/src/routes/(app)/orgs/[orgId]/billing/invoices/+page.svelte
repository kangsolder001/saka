<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createInfiniteInvoicesQuery } from '$lib/features/billing/api';
	import { createOrganizationQuery } from '$lib/features/org/api';
	import { can } from '$lib/features/org/permissions';
	import { formatMoney } from '$lib/format/money';
	import { formatDate } from '$lib/format/date';
	import StatusBadge from '$lib/components/app/StatusBadge.svelte';
	import LoadMore from '$lib/components/app/LoadMore.svelte';
	import EmptyState from '$lib/components/app/EmptyState.svelte';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import * as Table from '$lib/components/ui/table/index.js';
	import * as m from '$lib/paraglide/messages.js';

	let orgId = $derived(page.params.orgId ?? '');
	let orgQuery = createOrganizationQuery(() => orgId);
	let role = $derived(orgQuery.data?.organization?.role ?? 'member');
	let canViewInvoices = $derived(can(role, 'invoices.view'));

	let invoicesQuery = createInfiniteInvoicesQuery(() => orgId);

	let allInvoices = $derived(invoicesQuery.data?.pages.flatMap((page) => page.items) ?? []);

	function handleRowClick(invoiceId: string) {
		goto(`/orgs/${orgId}/billing/invoices/${invoiceId}`);
	}
</script>

<div class="space-y-6" data-testid="invoices-page">
	<PageHeader title={m.billing_invoices_title()} description={m.billing_invoices_desc()} />

	{#if orgQuery.isPending}
		<PageSkeleton />
	{:else if !canViewInvoices}
		<div
			class="rounded-lg border border-border bg-card p-6 text-card-foreground shadow-none"
			data-testid="invoices-forbidden"
		>
			<h3 class="text-base font-semibold text-foreground">
				{m.billing_invoices_forbidden_title()}
			</h3>
			<p class="mt-1 text-sm text-muted-foreground">
				{m.billing_invoices_forbidden_desc()}
			</p>
		</div>
	{:else if invoicesQuery.isPending}
		<PageSkeleton />
	{:else if invoicesQuery.isError}
		<ErrorState error={invoicesQuery.error} retry={() => invoicesQuery.refetch()} />
	{:else if allInvoices.length === 0}
		<EmptyState
			title={m.billing_invoices_empty_title()}
			description={m.billing_invoices_empty_desc()}
		/>
	{:else}
		<div class="overflow-x-auto rounded-lg border border-border">
			<Table.Root>
				<Table.Header>
					<Table.Row class="hover:bg-transparent">
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_number()}</Table.Head>
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_plan()}</Table.Head>
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_kind()}</Table.Head>
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_amount()}</Table.Head>
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_status()}</Table.Head>
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_due()}</Table.Head>
						<Table.Head class="whitespace-nowrap">{m.billing_invoices_col_paid()}</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body data-testid="invoices-table-body">
					{#each allInvoices as inv (inv.id)}
						<Table.Row
							class="cursor-pointer transition-colors hover:bg-muted/50"
							onclick={() => handleRowClick(inv.id)}
							data-testid={`invoice-row-${inv.id}`}
							tabindex={0}
							onkeydown={(e) => {
								if (e.key === 'Enter' || e.key === ' ') {
									e.preventDefault();
									handleRowClick(inv.id);
								}
							}}
						>
							<Table.Cell class="font-mono font-medium whitespace-nowrap">
								<a
									href={`/orgs/${orgId}/billing/invoices/${inv.id}`}
									class="hover:underline"
									onclick={(e) => e.stopPropagation()}
									data-testid={`invoice-link-${inv.id}`}
								>
									{inv.number}
								</a>
							</Table.Cell>
							<Table.Cell class="font-medium whitespace-nowrap">{inv.plan_name}</Table.Cell>
							<Table.Cell class="whitespace-nowrap text-muted-foreground">
								{inv.kind === 'checkout'
									? m.billing_invoice_kind_checkout()
									: m.billing_invoice_kind_renewal()}
							</Table.Cell>
							<Table.Cell class="font-medium whitespace-nowrap">
								{formatMoney(inv.amount, inv.currency)}
							</Table.Cell>
							<Table.Cell class="whitespace-nowrap">
								<StatusBadge kind="invoice" status={inv.status} />
							</Table.Cell>
							<Table.Cell class="whitespace-nowrap text-muted-foreground">
								{formatDate(inv.due_at)}
							</Table.Cell>
							<Table.Cell class="whitespace-nowrap text-muted-foreground">
								{inv.paid_at ? formatDate(inv.paid_at) : '-'}
							</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>

		<LoadMore
			hasMore={Boolean(invoicesQuery.hasNextPage)}
			loading={invoicesQuery.isFetchingNextPage}
			onLoadMore={() => invoicesQuery.fetchNextPage()}
		/>
	{/if}
</div>
