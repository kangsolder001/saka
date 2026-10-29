<script lang="ts">
	import { getLocale, setLocale, type Locale } from '$lib/i18n';
	import { Button } from '$lib/components/ui/button';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as m from '$lib/paraglide/messages';
	import { Globe } from 'lucide-svelte';

	interface Props {
		class?: string;
	}

	let { class: className = '' }: Props = $props();

	let currentLocale = $derived(getLocale());

	function changeLocale(loc: Locale): void {
		try {
			if (typeof window !== 'undefined' && window.localStorage) {
				window.localStorage.setItem('PARAGLIDE_LOCALE', loc);
			}
		} catch {
			// Ignore localStorage write failures (private mode / sandboxed iframe)
		}
		setLocale(loc);
	}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="outline"
				size="sm"
				class="h-10 min-h-[44px] min-w-[44px] gap-2 rounded-lg px-3 {className}"
				aria-label={m.common_language()}
			>
				<Globe class="h-4 w-4" />
				<span class="text-xs font-medium uppercase">{currentLocale}</span>
			</Button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		<DropdownMenu.Item
			onclick={() => changeLocale('id')}
			class={currentLocale === 'id' ? 'font-semibold text-primary' : ''}
		>
			<span>{m.common_language_id()}</span>
		</DropdownMenu.Item>
		<DropdownMenu.Item
			onclick={() => changeLocale('en')}
			class={currentLocale === 'en' ? 'font-semibold text-primary' : ''}
		>
			<span>{m.common_language_en()}</span>
		</DropdownMenu.Item>
	</DropdownMenu.Content>
</DropdownMenu.Root>
