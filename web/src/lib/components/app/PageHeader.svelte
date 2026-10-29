<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		description,
		actions,
		badge
	}: {
		title: string;
		description?: string;
		actions?: Snippet;
		badge?: Snippet;
	} = $props();
</script>

<div
	class="flex flex-col gap-4 border-b border-border pb-6 sm:flex-row sm:items-center sm:justify-between"
	data-testid="page-header"
>
	<div class="flex flex-col gap-1">
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
				{title}
			</h1>
			{#if badge}
				{@render badge()}
			{/if}
		</div>
		{#if description}
			<p class="text-sm text-muted-foreground">{description}</p>
		{/if}
	</div>

	{#if actions}
		<div class="flex flex-wrap items-center gap-3">
			{@render actions()}
		</div>
	{/if}
</div>
