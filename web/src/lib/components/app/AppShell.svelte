<script lang="ts">
	import { page } from '$app/state';
	import { CreditCard, LayoutDashboard, Menu, Settings, Users, X } from 'lucide-svelte';
	import * as Sheet from '$lib/components/ui/sheet';
	import OrgSwitcher from './OrgSwitcher.svelte';
	import UserMenu from './UserMenu.svelte';
	import * as m from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';

	let {
		activeOrgId = '',
		children
	}: {
		activeOrgId?: string;
		children?: Snippet;
	} = $props();

	let mobileOpen = $state(false);

	let navItems = $derived([
		{
			href: `/orgs/${activeOrgId}`,
			label: m.nav_overview ? m.nav_overview() : 'Ringkasan',
			icon: LayoutDashboard,
			exact: true
		},
		{
			href: `/orgs/${activeOrgId}/members`,
			label: m.nav_members ? m.nav_members() : 'Anggota',
			icon: Users,
			exact: false
		},
		{
			href: `/orgs/${activeOrgId}/billing`,
			label: m.nav_billing ? m.nav_billing() : 'Billing',
			icon: CreditCard,
			exact: false
		},
		{
			href: `/orgs/${activeOrgId}/settings`,
			label: m.nav_settings ? m.nav_settings() : 'Pengaturan',
			icon: Settings,
			exact: false
		}
	]);

	function isActive(href: string, exact: boolean): boolean {
		const currentPath = page.url.pathname;
		if (exact) {
			return currentPath === href || currentPath === `${href}/`;
		}
		return currentPath === href || currentPath.startsWith(`${href}/`);
	}
</script>

<div class="relative flex min-h-screen w-full bg-background font-sans text-foreground">
	<!-- Desktop Sidebar -->
	<aside
		class="fixed inset-y-0 left-0 z-30 hidden w-64 flex-col justify-between border-r border-sidebar-border bg-sidebar p-4 text-sidebar-foreground md:flex"
		data-testid="desktop-sidebar"
	>
		<div class="flex flex-col gap-6">
			<!-- App Logo / Title -->
			<div class="flex items-center gap-2 px-2 pt-2">
				<span class="text-xl font-bold tracking-tight text-primary">Saka</span>
			</div>

			<!-- Organization Switcher -->
			<OrgSwitcher {activeOrgId} />

			<!-- Navigation -->
			<nav class="flex flex-col gap-1" aria-label="Main Navigation">
				{#each navItems as item (item.href)}
					{@const active = isActive(item.href, item.exact)}
					<a
						href={item.href}
						class="flex min-h-[44px] items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors {active
							? 'bg-sidebar-primary text-sidebar-primary-foreground'
							: 'text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground'}"
						aria-current={active ? 'page' : undefined}
						data-testid={`nav-item-${item.label.toLowerCase()}`}
					>
						<item.icon class="h-4 w-4 shrink-0" aria-hidden="true" />
						<span>{item.label}</span>
					</a>
				{/each}
			</nav>
		</div>

		<!-- User Menu at bottom -->
		<div class="border-t border-sidebar-border pt-4">
			<UserMenu />
		</div>
	</aside>

	<!-- Mobile Header & Main -->
	<div class="flex min-h-screen w-full flex-col md:pl-64">
		<header
			class="sticky top-0 z-40 flex h-14 w-full items-center justify-between border-b border-border bg-background px-4 md:hidden"
			data-testid="mobile-header"
		>
			<Sheet.Root bind:open={mobileOpen}>
				<Sheet.Trigger>
					{#snippet child({ props })}
						<button
							{...props}
							class="flex h-11 w-11 items-center justify-center rounded-lg hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
							aria-label={m.nav_open_menu ? m.nav_open_menu() : 'Buka menu'}
							data-testid="mobile-menu-trigger"
						>
							<Menu class="h-5 w-5 text-foreground" aria-hidden="true" />
						</button>
					{/snippet}
				</Sheet.Trigger>
				<Sheet.Content
					side="left"
					class="flex w-72 flex-col justify-between border-r border-sidebar-border bg-sidebar p-4 text-sidebar-foreground"
					showCloseButton={false}
				>
					<div class="flex flex-col gap-6">
						<div class="flex items-center justify-between pt-2">
							<span class="text-xl font-bold tracking-tight text-primary">Saka</span>
							<Sheet.Close>
								{#snippet child({ props })}
									<button
										{...props}
										class="flex h-11 w-11 items-center justify-center rounded-lg text-sidebar-foreground hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-none"
										aria-label={m.nav_close_menu ? m.nav_close_menu() : 'Tutup menu'}
										data-testid="mobile-drawer-close"
									>
										<X class="h-5 w-5" aria-hidden="true" />
									</button>
								{/snippet}
							</Sheet.Close>
						</div>

						<OrgSwitcher {activeOrgId} />

						<nav class="flex flex-col gap-1" aria-label="Mobile Navigation">
							{#each navItems as item (item.href)}
								{@const active = isActive(item.href, item.exact)}
								<a
									href={item.href}
									onclick={() => (mobileOpen = false)}
									class="flex min-h-[44px] items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors {active
										? 'bg-sidebar-primary text-sidebar-primary-foreground'
										: 'text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground'}"
									aria-current={active ? 'page' : undefined}
								>
									<item.icon class="h-4 w-4 shrink-0" aria-hidden="true" />
									<span>{item.label}</span>
								</a>
							{/each}
						</nav>
					</div>

					<div class="border-t border-sidebar-border pt-4">
						<UserMenu />
					</div>
				</Sheet.Content>
			</Sheet.Root>

			<span class="font-semibold text-foreground">Saka</span>
			<div class="w-11"></div>
		</header>

		<!-- Main Content Area -->
		<main class="w-full flex-1 overflow-x-hidden">
			<div class="mx-auto max-w-5xl p-4 md:p-6">
				{#if children}
					{@render children()}
				{/if}
			</div>
		</main>
	</div>
</div>
