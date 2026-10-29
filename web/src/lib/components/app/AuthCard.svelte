<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as Card from '$lib/components/ui/card';

	interface Props {
		title?: Snippet | string;
		description?: Snippet | string;
		header?: Snippet;
		children?: Snippet;
		footer?: Snippet;
		class?: string;
	}

	let { title, description, header, children, footer, class: className = '' }: Props = $props();
</script>

<div class="w-full max-w-[400px]">
	<Card.Root
		class="w-full rounded-xl border border-border/80 bg-card p-6 text-card-foreground shadow-sm {className}"
	>
		{#if header}
			{@render header()}
		{:else if title || description}
			<Card.Header class="space-y-1.5 p-0 text-center">
				{#if title}
					{#if typeof title === 'string'}
						<Card.Title class="text-xl font-bold tracking-tight text-foreground md:text-2xl">
							{title}
						</Card.Title>
					{:else}
						{@render title()}
					{/if}
				{/if}
				{#if description}
					{#if typeof description === 'string'}
						<Card.Description class="text-sm text-muted-foreground">
							{description}
						</Card.Description>
					{:else}
						{@render description()}
					{/if}
				{/if}
			</Card.Header>
		{/if}

		{#if children}
			<Card.Content class="space-y-4 p-0 pt-6">
				{@render children()}
			</Card.Content>
		{/if}

		{#if footer}
			<Card.Footer class="flex flex-col gap-2 p-0 pt-6 text-center text-sm text-muted-foreground">
				{@render footer()}
			</Card.Footer>
		{/if}
	</Card.Root>
</div>
