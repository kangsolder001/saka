<script lang="ts">
	import { goto } from '$app/navigation';
	import { Loader2 } from 'lucide-svelte';
	import { createMeQuery } from '$lib/features/account/api';
	import { getLastOrgId } from '$lib/features/org/api';
	import * as m from '$lib/paraglide/messages';

	const meQuery = createMeQuery();

	$effect(() => {
		if (meQuery.isSuccess && meQuery.data) {
			const me = meQuery.data;
			const organizations = me.organizations ?? [];
			const lastOrgId = getLastOrgId();

			if (lastOrgId && organizations.some((org) => org.id === lastOrgId)) {
				void goto(`/orgs/${lastOrgId}`);
			} else if (organizations.length > 0 && organizations[0]?.id) {
				void goto(`/orgs/${organizations[0].id}`);
			} else {
				void goto('/orgs/new');
			}
		}
	});
</script>

<div class="flex min-h-[60vh] w-full flex-col items-center justify-center p-4">
	<div class="flex items-center gap-2 text-sm text-muted-foreground">
		<Loader2 class="size-5 animate-spin text-primary" />
		<span>{m.common_loading()}</span>
	</div>
</div>
