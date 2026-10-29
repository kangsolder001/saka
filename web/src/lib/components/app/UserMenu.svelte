<script lang="ts">
	import { goto } from '$app/navigation';
	import { ChevronsUpDown, LogOut, User } from 'lucide-svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as Avatar from '$lib/components/ui/avatar';
	import ThemeSwitcher from './ThemeSwitcher.svelte';
	import LanguageSwitcher from './LanguageSwitcher.svelte';
	import { session } from '$lib/auth/session.svelte';
	import { logout } from '$lib/auth/logout';
	import * as m from '$lib/paraglide/messages';

	let user = $derived(session.user);
	let initials = $derived(
		user?.name
			? user.name
					.split(' ')
					.map((n) => n[0])
					.slice(0, 2)
					.join('')
					.toUpperCase()
			: 'U'
	);
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<button
				{...props}
				class="flex h-12 w-full items-center justify-between gap-3 rounded-lg p-2 text-left transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-none"
				aria-label="User menu"
				data-testid="user-menu-trigger"
			>
				<span class="flex min-w-0 items-center gap-3">
					<Avatar.Root class="h-8 w-8 shrink-0">
						<Avatar.Fallback
							class="bg-sidebar-primary text-xs font-semibold text-sidebar-primary-foreground"
						>
							{initials}
						</Avatar.Fallback>
					</Avatar.Root>
					<span class="flex min-w-0 flex-col">
						<span class="truncate text-sm font-semibold text-sidebar-foreground">
							{user?.name ?? 'User'}
						</span>
						<span class="truncate text-xs text-muted-foreground">
							{user?.email ?? ''}
						</span>
					</span>
				</span>
				<ChevronsUpDown class="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
			</button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content class="w-60" align="start">
		<DropdownMenu.Group>
			<div class="px-2 py-1.5">
				<p class="truncate text-sm font-semibold text-foreground">{user?.name ?? 'User'}</p>
				<p class="truncate text-xs text-muted-foreground">{user?.email ?? ''}</p>
			</div>
		</DropdownMenu.Group>
		<DropdownMenu.Separator />
		<DropdownMenu.Group>
			<DropdownMenu.Item
				class="flex min-h-[44px] cursor-pointer items-center gap-2 text-sm"
				onclick={() => goto('/account')}
				data-testid="user-menu-account-link"
			>
				<User class="h-4 w-4" aria-hidden="true" />
				<span>{m.nav_account ? m.nav_account() : 'Akun'}</span>
			</DropdownMenu.Item>
		</DropdownMenu.Group>
		<DropdownMenu.Separator />
		<div class="flex items-center justify-between px-2 py-2">
			<span class="text-xs font-medium text-muted-foreground">
				{m.common_theme ? m.common_theme() : 'Tema'}
			</span>
			<ThemeSwitcher />
		</div>
		<div class="flex items-center justify-between px-2 py-2">
			<span class="text-xs font-medium text-muted-foreground">
				{m.common_language ? m.common_language() : 'Bahasa'}
			</span>
			<LanguageSwitcher />
		</div>
		<DropdownMenu.Separator />
		<DropdownMenu.Item
			class="flex min-h-[44px] cursor-pointer items-center gap-2 text-sm text-destructive focus:text-destructive"
			onclick={() => logout()}
			data-testid="user-menu-logout-button"
		>
			<LogOut class="h-4 w-4" aria-hidden="true" />
			<span>{m.nav_logout ? m.nav_logout() : 'Keluar'}</span>
		</DropdownMenu.Item>
	</DropdownMenu.Content>
</DropdownMenu.Root>
