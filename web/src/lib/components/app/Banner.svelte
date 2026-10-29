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
		info: 'bg-info-muted text-info border-info/30',
		warning: 'bg-warning-muted text-warning border-warning/30',
		neutral: 'bg-muted/70 text-muted-foreground border-border'
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
