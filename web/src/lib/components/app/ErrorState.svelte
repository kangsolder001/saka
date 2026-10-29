<script lang="ts">
	import { AlertTriangle, RotateCcw } from 'lucide-svelte';
	import { errorMessage } from '$lib/api/error-message';
	import { ApiError } from '$lib/api/errors';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';

	let {
		error,
		title,
		description,
		retry,
		action
	}: {
		error?: unknown;
		title?: string;
		description?: string;
		retry?: () => void;
		action?: Snippet;
	} = $props();

	let resolvedMessage = $derived(
		description ??
			(error
				? errorMessage(error)
				: m.error_internal_error
					? m.error_internal_error()
					: 'Terjadi kesalahan pada server.')
	);

	let traceId = $derived(error instanceof ApiError && error.traceId ? error.traceId : null);
</script>

<div
	class="flex min-h-[280px] w-full flex-col items-center justify-center rounded-xl border border-border bg-card p-8 text-center text-card-foreground"
	data-testid="error-state"
>
	<div
		class="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-danger-muted text-destructive"
	>
		<AlertTriangle class="h-6 w-6" aria-hidden="true" />
	</div>

	<h3 class="text-base font-semibold text-foreground">
		{title ?? (m.error_not_found ? m.error_not_found() : 'Terjadi Kesalahan')}
	</h3>

	<p class="mt-1 max-w-md text-sm text-muted-foreground">
		{resolvedMessage}
	</p>

	{#if traceId}
		<div class="mt-2 font-mono text-xs text-muted-foreground" data-testid="error-trace-id">
			{m.state_trace_id ? m.state_trace_id() : 'Trace ID'}: {traceId}
		</div>
	{/if}

	{#if retry || action}
		<div class="mt-6 flex flex-wrap items-center justify-center gap-3">
			{#if retry}
				<Button
					variant="outline"
					class="min-h-[44px] gap-2"
					onclick={retry}
					data-testid="error-retry-button"
				>
					<RotateCcw class="h-4 w-4" aria-hidden="true" />
					<span>{m.state_retry ? m.state_retry() : 'Coba lagi'}</span>
				</Button>
			{/if}
			{#if action}
				{@render action()}
			{/if}
		</div>
	{/if}
</div>
