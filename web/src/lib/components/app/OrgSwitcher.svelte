<script lang="ts">
	import { goto } from '$app/navigation';
	import { Building2, Check, ChevronsUpDown, Plus } from 'lucide-svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { createMeQuery } from '$lib/features/account/api';
	import { setLastOrgId } from '$lib/features/org/api';
	import * as m from '$lib/paraglide/messages';

	let { activeOrgId = '' }: { activeOrgId?: string } = $props();

	const meQuery = createMeQuery();

	let organizations = $derived(meQuery.data?.organizations ?? []);
	let currentOrg = $derived(
		organizations.find((o: { id: string; name: string }) => o.id === activeOrgId)
	);

	function handleSelect(orgId: string) {
		setLastOrgId(orgId);
		void goto(`/orgs/${orgId}`);
	}

	function handleCreateNew() {
		void goto('/orgs/new');
	}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<button
				{...props}
				class="flex h-11 w-full items-center justify-between gap-2 rounded-lg border border-sidebar-border bg-sidebar px-3 text-left text-sm font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-none"
				aria-label={m.org_switcher_select ? m.org_switcher_select() : 'Pilih organisasi'}
				data-testid="org-switcher-trigger"
			>
				<span class="flex min-w-0 items-center gap-2">
					<span
						class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-sidebar-primary text-xs font-semibold text-sidebar-primary-foreground"
					>
						<Building2 class="h-4 w-4" aria-hidden="true" />
					</span>
					<span class="truncate font-semibold text-sidebar-foreground">
						{currentOrg?.name ??
							(m.org_switcher_select ? m.org_switcher_select() : 'Pilih organisasi')}
					</span>
				</span>
				<ChevronsUpDown class="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
			</button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content class="w-56" align="start">
		<DropdownMenu.Group>
			<DropdownMenu.GroupHeading>
				{m.org_switcher_select ? m.org_switcher_select() : 'Organisasi'}
			</DropdownMenu.GroupHeading>
			{#each organizations as org (org.id)}
				<DropdownMenu.Item
					class="flex min-h-[44px] cursor-pointer items-center justify-between text-sm"
					onclick={() => handleSelect(org.id)}
					data-testid={`org-item-${org.id}`}
				>
					<span class="truncate font-medium">{org.name}</span>
					{#if org.id === activeOrgId}
						<Check class="h-4 w-4 text-primary" aria-hidden="true" />
					{/if}
				</DropdownMenu.Item>
			{/each}
		</DropdownMenu.Group>
		<DropdownMenu.Separator />
		<DropdownMenu.Item
			class="flex min-h-[44px] cursor-pointer items-center gap-2 text-sm text-primary"
			onclick={handleCreateNew}
			data-testid="org-create-new-button"
		>
			<Plus class="h-4 w-4" aria-hidden="true" />
			<span>{m.org_create_new ? m.org_create_new() : 'Buat organisasi baru'}</span>
		</DropdownMenu.Item>
	</DropdownMenu.Content>
</DropdownMenu.Root>
