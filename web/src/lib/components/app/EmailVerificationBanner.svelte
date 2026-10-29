<script lang="ts">
	import { Mail } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';
	import { call, client } from '$lib/api/client.js';
	import { errorMessage } from '$lib/api/error-message.js';
	import Banner from '$lib/components/app/Banner.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as m from '$lib/paraglide/messages.js';

	interface Props {
		class?: string;
	}

	let { class: className }: Props = $props();

	let sending = $state(false);

	async function handleResend() {
		if (sending) return;
		sending = true;
		try {
			await call(() => client.POST('/auth/resend-verification'));
			toast.success(m.banner_resend_success());
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			sending = false;
		}
	}
</script>

<Banner variant="warning" class={className}>
	<Mail class="size-4 shrink-0 text-warning" />
	<span>{m.banner_email_unverified_msg()}</span>

	{#snippet action()}
		<Button
			variant="outline"
			size="sm"
			disabled={sending}
			onclick={handleResend}
			class="h-7 border-warning/40 bg-warning-muted text-warning hover:bg-warning/20"
		>
			{sending ? m.banner_resending() : m.banner_resend_verification()}
		</Button>
	{/snippet}
</Banner>
