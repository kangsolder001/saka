<script lang="ts">
	import type { Role, Subscription } from '$lib/api/types';
	import {
		createSubscriptionQuery,
		resumeSubscription,
		changePlan
	} from '$lib/features/billing/api';
	import { subscriptionBanner } from '$lib/features/billing/banners';
	import { can } from '$lib/features/org/permissions';
	import Banner from '$lib/components/app/Banner.svelte';
	import { Button } from '$lib/components/ui/button';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		orgId: string;
		role?: Role;
		subscription?: Subscription | null;
		class?: string;
	}

	let { orgId, role, subscription, class: className }: Props = $props();

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	// Only query when subscription is not explicitly passed as prop
	let query = createSubscriptionQuery(
		() => (subscription === undefined ? orgId : ''),
		() => qc
	);
	let sub = $derived(subscription !== undefined ? subscription : (query.data ?? null));
	let banner = $derived(subscriptionBanner(sub));
	let isOwner = $derived(can(role, 'billing.manage'));
	let isExecuting = $state(false);

	async function handleAction() {
		if (!banner || !isOwner || isExecuting) return;

		switch (banner.actionType) {
			case 'invoices':
				await goto(`/orgs/${orgId}/billing/invoices`);
				break;
			case 'plans':
				await goto(`/orgs/${orgId}/billing/plans`);
				break;
			case 'resume':
				try {
					isExecuting = true;
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
					isExecuting = false;
				}
				break;
			case 'cancel_pending':
				if (!sub?.plan) return;
				try {
					isExecuting = true;
					await changePlan(orgId, { plan: sub.plan });
					qc?.invalidateQueries({ queryKey: queryKeys.subscription(orgId) });
					toast.success(
						m.billing_change_cleared_toast
							? m.billing_change_cleared_toast()
							: 'Perubahan plan dibatalkan'
					);
				} catch {
					toast.error(
						m.error_internal_error ? m.error_internal_error() : 'Terjadi kesalahan pada server.'
					);
				} finally {
					isExecuting = false;
				}
				break;
		}
	}
</script>

{#if banner}
	<div data-testid="subscription-banner-wrapper">
		<Banner variant={banner.variant} class={className}>
			<span data-testid="subscription-banner-message">{banner.message}</span>
			{#snippet action()}
				{#if isOwner}
					<Button
						variant="outline"
						size="sm"
						class="min-h-[44px]"
						disabled={isExecuting}
						onclick={handleAction}
						data-testid="subscription-banner-action"
					>
						{banner.actionLabel}
					</Button>
				{/if}
			{/snippet}
		</Banner>
	</div>
{/if}
