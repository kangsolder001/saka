<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { ArrowLeft } from 'lucide-svelte';
	import { createInvoiceQuery } from '$lib/features/billing/api';
	import { goToPayment } from '$lib/features/billing/purchase';
	import FakePaymentDialog from '$lib/features/billing/components/FakePaymentDialog.svelte';
	import StatusBadge from '$lib/components/app/StatusBadge.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { formatMoney } from '$lib/format/money';
	import { formatDate } from '$lib/format/date';
	import * as m from '$lib/paraglide/messages.js';

	let orgId = $derived(page.params.orgId ?? '');
	let invoiceId = $derived(page.params.invoiceId ?? '');

	let invoiceQuery = createInvoiceQuery(
		() => orgId,
		() => invoiceId
	);

	let invoice = $derived(invoiceQuery.data);

	let canPay = $derived(invoice && invoice.status === 'open' && Boolean(invoice.payment_url));

	function handleBack() {
		goto(`/orgs/${orgId}/billing/invoices`);
	}

	function handlePay() {
		if (!invoice) return;
		goToPayment({ orgId, invoice });
	}
</script>

<div class="space-y-6" data-testid="invoice-detail-page">
	<!-- Back link -->
	<div>
		<Button
			variant="ghost"
			class="-ml-2 min-h-[44px] gap-2 text-muted-foreground hover:text-foreground"
			onclick={handleBack}
			data-testid="back-button"
		>
			<ArrowLeft class="h-4 w-4" aria-hidden="true" />
			<span>{m.billing_invoice_detail_back()}</span>
		</Button>
	</div>

	{#if invoiceQuery.isPending}
		<PageSkeleton />
	{:else if invoiceQuery.isError}
		<ErrorState error={invoiceQuery.error} retry={() => invoiceQuery.refetch()} />
	{:else if invoice}
		<!-- Header -->
		<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<div class="flex items-center gap-3">
					<h1
						class="text-2xl font-bold tracking-tight text-foreground"
						data-testid="invoice-number"
					>
						Invoice #{invoice.number}
					</h1>
					<StatusBadge kind="invoice" status={invoice.status} />
				</div>
				<p class="mt-1 text-xs text-muted-foreground" data-testid="invoice-created-at">
					{m.billing_invoice_detail_created_at()}: {formatDate(invoice.created_at)}
				</p>
			</div>

			{#if canPay}
				<div>
					<Button
						variant="default"
						class="min-h-[44px] w-full sm:w-auto"
						onclick={handlePay}
						data-testid="pay-invoice-btn"
					>
						{m.billing_invoice_pay_now()}
					</Button>
				</div>
			{/if}
		</div>

		<!-- Details card -->
		<div
			class="rounded-lg border border-border bg-card p-6 text-card-foreground shadow-none"
			data-testid="invoice-metadata-card"
		>
			<div class="border-b border-border pb-6">
				<p class="text-xs font-semibold tracking-wider text-muted-foreground uppercase">
					{m.billing_invoices_col_amount()}
				</p>
				<p
					class="mt-1 text-3xl font-bold tracking-tight text-foreground"
					data-testid="invoice-amount"
				>
					{formatMoney(invoice.amount, invoice.currency)}
				</p>
			</div>

			<dl class="grid grid-cols-1 gap-6 pt-6 sm:grid-cols-2 lg:grid-cols-3">
				<div>
					<dt class="text-xs text-muted-foreground">{m.billing_invoices_col_plan()}</dt>
					<dd class="mt-1 text-sm font-medium text-foreground" data-testid="invoice-plan">
						{invoice.plan_name}
					</dd>
				</div>

				<div>
					<dt class="text-xs text-muted-foreground">{m.billing_invoices_col_kind()}</dt>
					<dd class="mt-1 text-sm font-medium text-foreground" data-testid="invoice-kind">
						{invoice.kind === 'checkout'
							? m.billing_invoice_kind_checkout()
							: m.billing_invoice_kind_renewal()}
					</dd>
				</div>

				<div>
					<dt class="text-xs text-muted-foreground">{m.billing_invoice_detail_period()}</dt>
					<dd class="mt-1 text-sm font-medium text-foreground" data-testid="invoice-period">
						{formatDate(invoice.period_start)} – {formatDate(invoice.period_end)}
					</dd>
				</div>

				<div>
					<dt class="text-xs text-muted-foreground">{m.billing_invoice_detail_due_at()}</dt>
					<dd class="mt-1 text-sm font-medium text-foreground" data-testid="invoice-due-at">
						{formatDate(invoice.due_at)}
					</dd>
				</div>

				<div>
					<dt class="text-xs text-muted-foreground">{m.billing_invoice_detail_paid_at()}</dt>
					<dd class="mt-1 text-sm font-medium text-foreground" data-testid="invoice-paid-at">
						{invoice.paid_at ? formatDate(invoice.paid_at) : '-'}
					</dd>
				</div>

				<div>
					<dt class="text-xs text-muted-foreground">{m.billing_invoices_col_status()}</dt>
					<dd class="mt-1">
						<StatusBadge kind="invoice" status={invoice.status} />
					</dd>
				</div>
			</dl>
		</div>
	{/if}

	<FakePaymentDialog {orgId} />
</div>
