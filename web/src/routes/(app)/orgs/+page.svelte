<script lang="ts">
	import { ArrowRight, Plus } from 'lucide-svelte';
	import AppShell from '$lib/components/app/AppShell.svelte';
	import EmptyState from '$lib/components/app/EmptyState.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import RoleBadge from '$lib/components/app/RoleBadge.svelte';
	import UserAvatar from '$lib/components/app/UserAvatar.svelte';
	import { Button } from '$lib/components/ui/button';
	import { createMeQuery } from '$lib/features/account/api';
	import { setLastOrgId } from '$lib/features/org/api';
	import * as m from '$lib/paraglide/messages';

	const meQuery = createMeQuery();

	let organizations = $derived(meQuery.data?.organizations ?? []);
</script>

<AppShell>
	{#if meQuery.isPending}
		<PageSkeleton />
	{:else if meQuery.isError}
		<ErrorState error={meQuery.error} retry={() => meQuery.refetch()} />
	{:else}
		<div class="flex flex-col gap-6">
			<PageHeader
				title={m.orgs_list_title()}
				description={m.orgs_list_desc ? m.orgs_list_desc() : undefined}
			>
				{#snippet actions()}
					<Button href="/orgs/new" class="min-h-[44px] gap-2" data-testid="create-org-btn">
						<Plus class="size-4" aria-hidden="true" />
						<span>{m.org_new_title ? m.org_new_title() : 'Create Organization'}</span>
					</Button>
				{/snippet}
			</PageHeader>

			{#if organizations.length === 0}
				<EmptyState
					title={m.orgs_list_empty_title ? m.orgs_list_empty_title() : undefined}
					description={m.orgs_list_empty_desc ? m.orgs_list_empty_desc() : undefined}
				>
					{#snippet action()}
						<Button href="/orgs/new" class="min-h-[44px] gap-2" data-testid="empty-create-org-btn">
							<Plus class="size-4" aria-hidden="true" />
							<span>{m.org_new_title ? m.org_new_title() : 'Create Organization'}</span>
						</Button>
					{/snippet}
				</EmptyState>
			{:else}
				<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" data-testid="orgs-grid">
					{#each organizations as org (org.id)}
						<div
							class="flex flex-col justify-between rounded-lg border border-border bg-card p-5"
							data-testid={`org-card-${org.id}`}
						>
							<div class="flex items-start gap-3.5">
								<UserAvatar
									name={org.name}
									avatarUrl={'logo_url' in org &&
									typeof (org as { logo_url?: unknown }).logo_url === 'string'
										? (org as { logo_url: string }).logo_url
										: null}
									size="lg"
								/>
								<div class="min-w-0 flex-1">
									<div class="flex items-start justify-between gap-2">
										<a
											href={`/orgs/${org.id}`}
											onclick={() => setLastOrgId(org.id)}
											class="truncate rounded-sm font-semibold text-foreground hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
										>
											{org.name}
										</a>
										<RoleBadge role={org.role} />
									</div>
									<p class="mt-0.5 truncate text-xs text-muted-foreground">
										{org.slug}
									</p>
								</div>
							</div>
							<div class="mt-4 flex items-center justify-end">
								<Button
									href={`/orgs/${org.id}`}
									onclick={() => setLastOrgId(org.id)}
									variant="outline"
									size="sm"
									class="min-h-[36px] gap-1.5"
									data-testid={`org-link-${org.id}`}
								>
									<span>{m.nav_overview ? m.nav_overview() : 'Overview'}</span>
									<ArrowRight class="size-3.5" aria-hidden="true" />
								</Button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</AppShell>
