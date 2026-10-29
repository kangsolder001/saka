<script lang="ts">
	import type { Snippet } from 'svelte';
	import ThemeSwitcher from '$lib/components/app/ThemeSwitcher.svelte';
	import LanguageSwitcher from '$lib/components/app/LanguageSwitcher.svelte';

	let { children }: { children: Snippet } = $props();
</script>

<div
	class="relative flex min-h-screen w-full flex-col items-center justify-center bg-background p-4 text-foreground selection:bg-primary/20"
>
	<!-- Top right theme and language controls per §4.4 -->
	<header class="absolute top-4 right-4 flex items-center gap-2">
		<LanguageSwitcher />
		<ThemeSwitcher />
	</header>

	<!-- Main auth container centered on screen -->
	<main class="flex w-full flex-col items-center justify-center">
		<!-- Brand logo header above the card per §4.4 -->
		<div class="mb-6 flex flex-col items-center gap-2">
			<a
				href="/"
				class="flex items-center gap-2 text-2xl font-bold tracking-tight text-primary transition-opacity hover:opacity-90"
			>
				<span>Saka</span>
			</a>
		</div>

		{@render children()}
	</main>
</div>
