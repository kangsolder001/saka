<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createOrganizationQuery, setLastOrgId } from '$lib/features/org/api';
	import SubscriptionBanner from '$lib/features/billing/components/SubscriptionBanner.svelte';
	import AppShell from '$lib/components/app/AppShell.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	let orgId = $derived(page.params.orgId ?? '');
	let orgQuery = createOrganizationQuery(() => orgId);

	$effect(() => {
		if (orgQuery.data?.organization?.id) {
			setLastOrgId(orgQuery.data.organization.id);
		}
	});
	let org = $derived(orgQuery.data?.organization);
</script>

{#if orgQuery.isPending}
	<div class="p-6">
		<PageSkeleton />
	</div>
{:else if orgQuery.isError}
	<div class="mx-auto flex min-h-[50vh] max-w-lg items-center justify-center p-6">
		<ErrorState
			error={orgQuery.error}
			title={m.org_not_found_title ? m.org_not_found_title() : 'Organisasi tidak ditemukan'}
			description={m.org_not_found_desc
				? m.org_not_found_desc()
				: 'Organisasi yang Anda tuju tidak ditemukan atau Anda bukan anggota.'}
			retry={() => orgQuery.refetch()}
		>
			{#snippet action()}
				<Button
					variant="default"
					class="min-h-[44px]"
					onclick={() => goto('/orgs')}
					data-testid="back-to-orgs-btn"
				>
					{m.org_back_to_list ? m.org_back_to_list() : 'Kembali ke daftar organisasi'}
				</Button>
			{/snippet}
		</ErrorState>
	</div>
{:else}
	<AppShell activeOrgId={orgId}>
		{#if org}
			<SubscriptionBanner orgId={org.id} role={org.role} class="mb-6" />
		{/if}
		{@render children()}
	</AppShell>
{/if}
