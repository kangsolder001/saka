<script lang="ts">
	import { cn } from '$lib/utils';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		label: string;
		used: number;
		max: number;
		unit?: string;
		class?: string;
	}

	let { label, used, max, unit, class: className }: Props = $props();

	let percentage = $derived(
		max > 0 ? Math.min(100, Math.max(0, Math.round((used / max) * 100))) : 0
	);

	let isLimitReached = $derived(max > 0 && percentage >= 100);
	let isWarning = $derived(max > 0 && percentage >= 80 && percentage < 100);

	let barColorClass = $derived.by(() => {
		if (isLimitReached) return 'bg-destructive';
		if (isWarning) return 'bg-warning';
		return 'bg-primary';
	});

	let textColorClass = $derived.by(() => {
		if (isLimitReached) return 'text-destructive';
		if (isWarning) return 'text-warning';
		return 'text-muted-foreground';
	});
</script>

<div class={cn('flex flex-col gap-2', className)} data-slot="usage-meter" data-testid="usage-meter">
	<div class="flex items-center justify-between text-sm font-medium">
		<span class="text-foreground" data-testid="usage-meter-label">{label}</span>
		<div class="flex items-center gap-2">
			{#if max > 0}
				<span class={cn('font-semibold', textColorClass)} data-testid="usage-meter-percentage">
					{percentage}%
				</span>
			{/if}
			<span
				class={cn(
					'text-xs',
					max > 0 && (isLimitReached || isWarning) ? textColorClass : 'text-muted-foreground'
				)}
				data-testid="usage-meter-count"
			>
				{#if max === -1}
					{used}{unit ? ` ${unit}` : ''} / {m.billing_unlimited
						? m.billing_unlimited()
						: 'Tak terbatas'}
				{:else}
					{used} / {max}{unit ? ` ${unit}` : ''}
				{/if}
			</span>
		</div>
	</div>

	{#if max === -1}
		<div class="h-2 w-full rounded-full bg-muted/40" data-testid="usage-meter-unlimited-bar"></div>
	{:else}
		<div
			role="progressbar"
			aria-valuenow={used}
			aria-valuemin={0}
			aria-valuemax={max}
			aria-label={label}
			class="h-2 w-full overflow-hidden rounded-full bg-muted"
			data-testid="usage-meter-track"
		>
			<div
				class={cn('h-full transition-all duration-300', barColorClass)}
				style={`width: ${percentage}%`}
				data-testid="usage-meter-fill"
			></div>
		</div>
	{/if}
</div>
