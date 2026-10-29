<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import { cn } from '$lib/utils';
	import * as m from '$lib/paraglide/messages';

	let { children }: { children: Snippet } = $props();

	const tabs = [
		{ href: '/account', label: m.account_tab_profile(), exact: true },
		{ href: '/account/security', label: m.account_tab_security(), exact: false },
		{ href: '/account/sessions', label: m.account_tab_sessions(), exact: false },
		{ href: '/account/delete', label: m.account_tab_delete(), exact: false }
	];

	function isTabActive(href: string, exact: boolean): boolean {
		const currentPath = page.url.pathname;
		if (exact) {
			return currentPath === href || currentPath === `${href}/`;
		}
		return currentPath.startsWith(href);
	}
</script>

<div class="mx-auto flex w-full max-w-5xl flex-col gap-8 p-4 sm:p-6 lg:p-8">
	<PageHeader title={m.account_title()} />

	<div class="flex flex-col gap-6 md:flex-row md:items-start md:gap-8">
		<!-- Subnav Tabs -->
		<nav
			class="flex shrink-0 flex-row gap-1 overflow-x-auto border-b border-border pb-2 md:w-56 md:flex-col md:border-r md:border-b-0 md:pr-4 md:pb-0"
			aria-label={m.account_title()}
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
				>
					{tab.label}
				</a>
			{/each}
		</nav>

		<!-- Tab Content -->
		<div class="min-w-0 flex-1">
			{@render children()}
		</div>
	</div>
</div>
