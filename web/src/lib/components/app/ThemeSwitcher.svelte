<script lang="ts">
	import { userPrefersMode, setMode } from 'mode-watcher';
	import { Button } from '$lib/components/ui/button';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as m from '$lib/paraglide/messages';
	import { Sun, Moon, Laptop } from 'lucide-svelte';

	interface Props {
		class?: string;
	}

	let { class: className = '' }: Props = $props();
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="outline"
				size="icon"
				class="h-10 min-h-[44px] w-10 min-w-[44px] rounded-lg {className}"
				aria-label={m.common_theme()}
			>
				<Sun
					class="h-[1.2rem] w-[1.2rem] scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90"
				/>
				<Moon
					class="absolute h-[1.2rem] w-[1.2rem] scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0"
				/>
				<span class="sr-only">{m.common_theme()}</span>
			</Button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		<DropdownMenu.Item
			onclick={() => setMode('light')}
			class={userPrefersMode.current === 'light' ? 'font-semibold text-primary' : ''}
		>
			<Sun class="mr-2 h-4 w-4" />
			<span>{m.common_theme_light()}</span>
		</DropdownMenu.Item>
		<DropdownMenu.Item
			onclick={() => setMode('dark')}
			class={userPrefersMode.current === 'dark' ? 'font-semibold text-primary' : ''}
		>
			<Moon class="mr-2 h-4 w-4" />
			<span>{m.common_theme_dark()}</span>
		</DropdownMenu.Item>
		<DropdownMenu.Item
			onclick={() => setMode('system')}
			class={userPrefersMode.current === 'system' ? 'font-semibold text-primary' : ''}
		>
			<Laptop class="mr-2 h-4 w-4" />
			<span>{m.common_theme_system()}</span>
		</DropdownMenu.Item>
	</DropdownMenu.Content>
</DropdownMenu.Root>
