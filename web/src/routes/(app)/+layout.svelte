<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { session } from '$lib/auth/session.svelte';
	import { ensureSession } from '$lib/auth/guards';
	import { setOnSessionEnded } from '$lib/api/client';
	import { queryClient } from '$lib/api/query-client';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages';
	import { Loader2 } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	onMount(() => {
		setOnSessionEnded(() => {
			queryClient.clear();
			toast.error(m.auth_session_ended ? m.auth_session_ended() : 'Sesi berakhir');
			goto('/login');
		});

		void ensureSession();
	});

	onDestroy(() => {
		setOnSessionEnded(null);
	});

	$effect(() => {
		if (session.status === 'anonymous') {
			const currentPath = page.url.pathname + page.url.search;
			const redirectParam = encodeURIComponent(currentPath);
			void goto(`/login?redirect=${redirectParam}`);
		}
	});
</script>

{#if session.status === 'unknown'}
	<div
		class="flex min-h-screen w-full flex-col items-center justify-center bg-background p-4 text-foreground"
		data-testid="app-loading-screen"
	>
		<div class="flex flex-col items-center gap-4">
			<div class="flex items-center gap-2">
				<span class="text-2xl font-bold tracking-tight text-primary">Saka</span>
			</div>
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<Loader2 class="h-5 w-5 animate-spin text-primary" />
				<span>{m.common_loading ? m.common_loading() : 'Memuat...'}</span>
			</div>
		</div>
	</div>
{:else if session.status === 'authenticated'}
	{@render children()}
{/if}
