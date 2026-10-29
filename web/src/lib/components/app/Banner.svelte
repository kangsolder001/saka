<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils.js';

	type BannerVariant = 'info' | 'warning' | 'neutral';

	interface Props {
		variant?: BannerVariant;
		class?: string;
		children?: Snippet;
		action?: Snippet;
	}

	let { variant = 'info', class: className, children, action }: Props = $props();

	const variantStyles: Record<BannerVariant, string> = {
		info: 'bg-blue-50 text-blue-900 border-blue-200 dark:bg-blue-950/40 dark:text-blue-200 dark:border-blue-900',
		warning:
			'bg-amber-50 text-amber-900 border-amber-200 dark:bg-amber-950/40 dark:text-amber-200 dark:border-amber-900',
		neutral:
			'bg-muted/70 text-muted-foreground border-border dark:bg-muted/30 dark:text-muted-foreground dark:border-border'
	};
</script>

<aside
	role="status"
	data-slot="banner"
	data-variant={variant}
	class={cn(
		'flex flex-col justify-between gap-3 border px-4 py-2.5 text-xs font-medium transition-colors sm:flex-row sm:items-center sm:text-sm',
		variantStyles[variant],
		className
	)}
>
	<div class="flex items-center gap-2">
		{@render children?.()}
	</div>
	{#if action}
		<div class="flex shrink-0 items-center gap-2">
			{@render action()}
		</div>
	{/if}
</aside>
