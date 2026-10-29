<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { formatMoney } from '$lib/format/money';
	import { fakePaymentDialog, closeFakePaymentDialog } from '../fake-dialog.svelte';
	import type { Invoice } from '$lib/api/types';
	import * as m from '$lib/paraglide/messages.js';

	interface Props {
		open?: boolean;
		invoice?: Invoice | null;
		orgId?: string;
		onClose?: () => void;
		onPaid?: () => void;
	}

	let {
		open = $bindable(),
		invoice = undefined,
		orgId = undefined,
		onClose,
		onPaid
	}: Props = $props();

	let isControlled = $derived(open !== undefined);
	let isOpen = $derived(isControlled ? Boolean(open) : fakePaymentDialog.open);
	let currentInvoice = $derived(invoice !== undefined ? invoice : fakePaymentDialog.invoice);
	let currentOrgId = $derived(orgId !== undefined ? orgId : fakePaymentDialog.orgId);

	let paying = $state(false);
	let error = $state<string | null>(null);

	function handleOpenChange(nextOpen: boolean) {
		if (!nextOpen) {
			handleClose();
		}
	}

	function handleClose() {
		if (isControlled) {
			open = false;
		} else {
			closeFakePaymentDialog();
		}
		error = null;
		paying = false;
		onClose?.();
	}

	async function handlePay() {
		if (!currentInvoice || paying) return;
		paying = true;
		error = null;

		try {
			if (import.meta.env.DEV) {
				const { simulateFakePayment } = await import('$lib/dev/fake-payment');
				await simulateFakePayment({
					invoiceNumber: currentInvoice.number,
					amount: currentInvoice.amount,
					currency: currentInvoice.currency
				});
			}
			handleClose();
			onPaid?.();
			await goto('/billing/return');
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Gagal memproses pembayaran simulasi';
		} finally {
			paying = false;
		}
	}
</script>

{#if import.meta.env.DEV && isOpen && currentInvoice}
	<Dialog.Root open={isOpen} onOpenChange={handleOpenChange}>
		<Dialog.Content
			class="border border-border shadow-none sm:max-w-[480px]"
			data-testid="fake-payment-dialog"
		>
			<Dialog.Header>
				<Dialog.Title>{m.billing_fake_payment_title()}</Dialog.Title>
				<Dialog.Description>{m.billing_fake_payment_desc()}</Dialog.Description>
			</Dialog.Header>

			<div class="space-y-3 py-4 text-sm">
				<div class="flex justify-between border-b border-border pb-2">
					<span class="text-muted-foreground">{m.billing_fake_payment_invoice_label()}</span>
					<span class="font-mono font-medium text-foreground" data-testid="fake-invoice-number">
						{currentInvoice.number}
					</span>
				</div>

				<div class="flex justify-between border-b border-border pb-2">
					<span class="text-muted-foreground">{m.billing_fake_payment_plan_label()}</span>
					<span class="font-medium text-foreground" data-testid="fake-invoice-plan">
						{currentInvoice.plan_name}
					</span>
				</div>

				<div class="flex justify-between pt-1">
					<span class="font-medium text-foreground">{m.billing_fake_payment_amount_label()}</span>
					<span class="text-base font-bold text-foreground" data-testid="fake-invoice-amount">
						{formatMoney(currentInvoice.amount, currentInvoice.currency)}
					</span>
				</div>

				{#if error}
					<div
						class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive"
						data-testid="fake-payment-error"
					>
						{error}
					</div>
				{/if}
			</div>

			<Dialog.Footer class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
				<Button
					type="button"
					variant="outline"
					class="min-h-[44px]"
					disabled={paying}
					onclick={handleClose}
					data-testid="fake-cancel-btn"
				>
					{m.billing_fake_payment_cancel_btn()}
				</Button>
				<Button
					type="button"
					variant="default"
					class="min-h-[44px]"
					disabled={paying}
					onclick={handlePay}
					data-testid="fake-pay-btn"
				>
					{paying ? m.billing_fake_payment_paying() : m.billing_fake_payment_pay_btn()}
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
{/if}
