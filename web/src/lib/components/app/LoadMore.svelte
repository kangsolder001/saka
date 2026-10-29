<script lang="ts">
	import { Loader2 } from 'lucide-svelte';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';

	let {
		hasMore,
		loading = false,
		onLoadMore
	}: {
		hasMore: boolean;
		loading?: boolean;
		onLoadMore: () => void;
	} = $props();
</script>

{#if hasMore}
	<div class="flex w-full justify-center py-4" data-testid="load-more-container">
		<Button
			variant="outline"
			class="min-h-[44px] min-w-[140px] gap-2"
			disabled={loading}
			onclick={onLoadMore}
			data-testid="load-more-button"
		>
			{#if loading}
				<Loader2 class="h-4 w-4 animate-spin text-primary" aria-hidden="true" />
				<span>{m.state_loading ? m.state_loading() : 'Memuat...'}</span>
			{:else}
				<span>{m.state_load_more ? m.state_load_more() : 'Muat lebih banyak'}</span>
			{/if}
		</Button>
	</div>
{/if}
