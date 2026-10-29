<script lang="ts">
	import { Inbox } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';
	import type { Component, Snippet } from 'svelte';

	let {
		title,
		description,
		icon: Icon = Inbox,
		action
	}: {
		title?: string;
		description?: string;
		icon?: typeof Inbox;
		action?: Snippet;
	} = $props();
</script>

<div
	class="flex min-h-[280px] w-full flex-col items-center justify-center rounded-xl border border-dashed border-border bg-card p-8 text-center text-card-foreground"
	data-testid="empty-state"
>
	<div
		class="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted text-muted-foreground"
	>
		<Icon class="h-6 w-6" aria-hidden="true" />
	</div>
	<h3 class="text-base font-semibold text-foreground">
		{title ?? (m.state_empty_title ? m.state_empty_title() : 'Tidak ada data')}
	</h3>
	{#if description}
		<p class="mt-1 max-w-sm text-sm text-muted-foreground">
			{description}
		</p>
	{/if}
	{#if action}
		<div class="mt-6">
			{@render action()}
		</div>
	{/if}
</div>
