<script lang="ts">
	/* eslint-disable svelte/valid-prop-names-in-kit-pages */
	import { goto } from '$app/navigation';
	import { ensureSession } from '$lib/auth/guards';
	import { getInvoice } from '$lib/features/billing/api';
	import { queryKeys } from '$lib/api/query-keys';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { Button } from '$lib/components/ui/button';
	import { CheckCircle2, AlertCircle, Clock, Loader2 } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		pollInterval?: number;
		timeout?: number;
	}

	let { pollInterval = 3000, timeout = 60000 }: Props = $props();

	type ReturnStatus = 'loading' | 'paid' | 'invalid' | 'timeout';

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	let status = $state<ReturnStatus>('loading');
	let orgId = $state<string>('');

	$effect(() => {
		let isMounted = true;
		let timeoutId: ReturnType<typeof setTimeout> | null = null;
		const PENDING_PAYMENT_KEY = 'saka.pendingPayment';

		async function checkPayment() {
			if (typeof window === 'undefined') return;

			let raw: string | null;
			try {
				raw = sessionStorage.getItem(PENDING_PAYMENT_KEY);
			} catch {
				goto('/');
				return;
			}

			if (!raw) {
				goto('/');
				return;
			}

			let parsed: { orgId?: string; invoiceId?: string };
			try {
				parsed = JSON.parse(raw);
			} catch {
				try {
					sessionStorage.removeItem(PENDING_PAYMENT_KEY);
				} catch {
					// ignore
				}
				goto('/');
				return;
			}

			if (!parsed.orgId || !parsed.invoiceId) {
				try {
					sessionStorage.removeItem(PENDING_PAYMENT_KEY);
				} catch {
					// ignore
				}
				goto('/');
				return;
			}

			orgId = parsed.orgId;
			const invoiceId = parsed.invoiceId;

			// 3. Ensure user session is loaded
			const authenticated = await ensureSession();
			if (!isMounted) return;
			if (!authenticated) {
				goto('/login');
				return;
			}

			// 4. Poll getInvoice(orgId, invoiceId) every 3 seconds for up to 60 seconds
			const startTime = Date.now();
			const MAX_POLL_MS = timeout;
			const INTERVAL_MS = pollInterval;

			async function poll() {
				if (!isMounted) return;

				try {
					const invoice = await getInvoice(orgId, invoiceId);
					if (!isMounted) return;

					if (invoice.status === 'paid') {
						try {
							sessionStorage.removeItem(PENDING_PAYMENT_KEY);
						} catch {
							// ignore
						}
						await qc?.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
						await qc?.invalidateQueries({ queryKey: queryKeys.invoices(orgId) });
						status = 'paid';
						return;
					}

					if (invoice.status === 'expired' || invoice.status === 'void') {
						try {
							sessionStorage.removeItem(PENDING_PAYMENT_KEY);
						} catch {
							// ignore
						}
						status = 'invalid';
						return;
					}
				} catch {
					// temporary network failure; continue polling
				}

				if (Date.now() - startTime >= MAX_POLL_MS) {
					status = 'timeout';
					return;
				}

				timeoutId = setTimeout(poll, INTERVAL_MS);
			}

			await poll();
		}

		checkPayment();

		return () => {
			isMounted = false;
			if (timeoutId) clearTimeout(timeoutId);
		};
	});
</script>

<div
	class="w-full max-w-md rounded-lg border border-border bg-card p-6 text-card-foreground shadow-none"
	data-testid="billing-return-container"
>
	{#if status === 'loading'}
		<div
			class="flex flex-col items-center justify-center gap-4 py-8 text-center"
			data-testid="billing-return-loading"
		>
			<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" aria-hidden="true" />
			<p class="text-sm font-medium text-muted-foreground">
				{m.billing_return_checking ? m.billing_return_checking() : 'Memeriksa status pembayaran...'}
			</p>
		</div>
	{:else if status === 'paid'}
		<div
			class="flex flex-col items-center justify-center gap-4 text-center"
			data-testid="billing-return-paid"
		>
			<div
				class="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-50 text-emerald-600 dark:bg-emerald-950/50 dark:text-emerald-400"
			>
				<CheckCircle2 class="h-6 w-6" aria-hidden="true" />
			</div>
			<div class="flex flex-col gap-1">
				<h1 class="text-xl font-bold tracking-tight text-foreground">
					{m.billing_return_success_title
						? m.billing_return_success_title()
						: 'Pembayaran Berhasil'}
				</h1>
				<p class="text-sm text-muted-foreground">
					{m.billing_return_success_desc
						? m.billing_return_success_desc()
						: 'Terima kasih! Pembayaran Anda telah diterima dan langganan telah aktif.'}
				</p>
			</div>
			<Button
				href={`/orgs/${orgId}/billing`}
				class="mt-2 min-h-[44px] w-full"
				data-testid="billing-return-billing-btn"
			>
				{m.billing_return_btn_billing ? m.billing_return_btn_billing() : 'Ke Billing'}
			</Button>
		</div>
	{:else if status === 'invalid'}
		<div
			class="flex flex-col items-center justify-center gap-4 text-center"
			data-testid="billing-return-invalid"
		>
			<div
				class="flex h-12 w-12 items-center justify-center rounded-full bg-amber-50 text-amber-600 dark:bg-amber-950/50 dark:text-amber-400"
			>
				<AlertCircle class="h-6 w-6" aria-hidden="true" />
			</div>
			<div class="flex flex-col gap-1">
				<h1 class="text-xl font-bold tracking-tight text-foreground">
					{m.billing_return_invalid_title
						? m.billing_return_invalid_title()
						: 'Invoice Tidak Berlaku'}
				</h1>
				<p class="text-sm text-muted-foreground">
					{m.billing_return_invalid_desc
						? m.billing_return_invalid_desc()
						: 'Invoice tidak berlaku lagi'}
				</p>
			</div>
			<Button
				variant="outline"
				href={`/orgs/${orgId}/billing/plans`}
				class="mt-2 min-h-[44px] w-full"
				data-testid="billing-return-plans-btn"
			>
				{m.billing_return_btn_plans ? m.billing_return_btn_plans() : 'Pilih Plan'}
			</Button>
		</div>
	{:else if status === 'timeout'}
		<div
			class="flex flex-col items-center justify-center gap-4 text-center"
			data-testid="billing-return-timeout"
		>
			<div
				class="flex h-12 w-12 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-950/50 dark:text-blue-400"
			>
				<Clock class="h-6 w-6" aria-hidden="true" />
			</div>
			<div class="flex flex-col gap-1">
				<h1 class="text-xl font-bold tracking-tight text-foreground">
					{m.billing_return_timeout_title
						? m.billing_return_timeout_title()
						: 'Pembayaran Sedang Diproses'}
				</h1>
				<p class="text-sm text-muted-foreground">
					{m.billing_return_timeout_desc
						? m.billing_return_timeout_desc()
						: 'Pembayaran sedang diproses. Status akan diperbarui otomatis dan kami mengirim email konfirmasi.'}
				</p>
			</div>
			<Button
				href={`/orgs/${orgId}/billing`}
				class="mt-2 min-h-[44px] w-full"
				data-testid="billing-return-billing-btn"
			>
				{m.billing_return_btn_billing ? m.billing_return_btn_billing() : 'Ke Billing'}
			</Button>
		</div>
	{/if}
</div>
