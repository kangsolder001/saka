<script lang="ts">
	import { QueryClientProvider, type QueryClient } from '@tanstack/svelte-query';
	import { createQueryClient } from '$lib/api/query-client';
	import InviteDialog from './InviteDialog.svelte';
	import type { ComponentProps } from 'svelte';

	let {
		client = createQueryClient(),
		...restProps
	}: ComponentProps<typeof InviteDialog> & { client?: QueryClient } = $props();
</script>

<QueryClientProvider {client}>
	<InviteDialog {...restProps} />
</QueryClientProvider>
