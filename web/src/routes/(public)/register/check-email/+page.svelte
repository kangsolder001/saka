<script lang="ts">
	import { page } from '$app/state';
	import { Mail } from 'lucide-svelte';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';

	const email = $derived(page.url.searchParams.get('email') ?? '');
	const org = $derived(page.url.searchParams.get('org'));
</script>

<AuthCard
	title={m.auth_check_email_title()}
	description={org
		? m.auth_check_email_invited_desc({ organization: org })
		: m.auth_check_email_desc({ email: email || 'your email' })}
>
	<div class="flex flex-col items-center justify-center py-6 text-center">
		<div
			class="mb-4 flex size-14 items-center justify-center rounded-full bg-primary/10 text-primary"
		>
			<Mail class="size-7" />
		</div>
		<p class="max-w-xs text-sm text-muted-foreground">
			{org
				? m.auth_check_email_invited_desc({ organization: org })
				: m.auth_check_email_desc({ email })}
		</p>
	</div>

	{#snippet footer()}
		<Button href="/login" variant="outline" class="h-11 w-full">
			{m.auth_check_email_back_to_login()}
		</Button>
	{/snippet}
</AuthCard>
