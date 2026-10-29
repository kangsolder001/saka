<script lang="ts">
	import * as m from '$lib/paraglide/messages.js';
	import { cn } from '$lib/utils.js';

	interface Props {
		used: number;
		max?: number;
		class?: string;
	}

	let { used, max, class: className }: Props = $props();

	let label = $derived(
		max === undefined || max === -1
			? m.members_usage_unlimited({ used })
			: m.members_usage({ used, max })
	);
</script>

<div
	data-slot="member-usage"
	data-testid="member-usage"
	class={cn(
		'inline-flex items-center gap-1.5 rounded-md border border-border bg-muted/40 px-3 py-1.5 text-sm font-medium text-muted-foreground',
		className
	)}
>
	<span>{label}</span>
</div>
