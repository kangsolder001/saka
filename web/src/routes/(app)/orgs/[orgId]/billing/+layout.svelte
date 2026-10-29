<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import { cn } from '$lib/utils';
	import * as m from '$lib/paraglide/messages';

	let { children }: { children: Snippet } = $props();

	let orgId = $derived(page.params.orgId ?? '');

	let tabs = $derived([
		{
			href: `/orgs/${orgId}/billing`,
			label: m.billing_tab_overview(),
			exact: true,
			testId: 'billing-tab-overview'
		},
		{
			href: `/orgs/${orgId}/billing/plans`,
			label: m.billing_tab_plans(),
			exact: false,
			testId: 'billing-tab-plans'
		},
		{
			href: `/orgs/${orgId}/billing/invoices`,
			label: m.billing_tab_invoices(),
			exact: false,
			testId: 'billing-tab-invoices'
		}
	]);

	function isTabActive(href: string, exact: boolean): boolean {
		const currentPath = page.url.pathname;
		if (exact) {
			return currentPath === href || currentPath === `${href}/`;
		}
		return currentPath === href || currentPath.startsWith(`${href}/`);
	}
</script>

<div class="mx-auto flex w-full max-w-5xl flex-col gap-6" data-testid="billing-layout">
	<PageHeader title={m.billing_title()} description={m.billing_desc()} />

	<!-- Sub-navigation Tabs -->
	<nav
		class="flex shrink-0 flex-row gap-1 overflow-x-auto border-b border-border pb-2"
		aria-label={m.billing_title()}
	>
		{#each tabs as tab (tab.href)}
			{@const active = isTabActive(tab.href, tab.exact)}
			<a
				href={tab.href}
				class={cn(
					'inline-flex min-h-[44px] items-center rounded-md px-3 py-2 text-sm font-medium whitespace-nowrap transition-colors',
					active
						? 'bg-muted font-semibold text-foreground'
						: 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
				)}
				aria-current={active ? 'page' : undefined}
				data-testid={tab.testId}
			>
				{tab.label}
			</a>
		{/each}
	</nav>

	<div class="min-w-0 flex-1">
		{@render children()}
	</div>
</div>
