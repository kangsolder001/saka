<script lang="ts">
	import { page } from '$app/state';
	import { session } from '$lib/auth/session.svelte';
	import { verifyEmail } from '$lib/features/auth/api';
	import TokenActionPage from '$lib/features/auth/components/TokenActionPage.svelte';
	import * as m from '$lib/paraglide/messages';

	const token = $derived(page.url.searchParams.get('token') ?? '');
	const isAuthenticated = $derived(session.status === 'authenticated');

	async function performVerification() {
		if (!token) {
			throw new Error(m.field_error_token_invalid());
		}
		await verifyEmail(token);
		if (session.user) {
			session.user.email_verified = true;
		}
	}
</script>

<TokenActionPage
	title={m.auth_verify_email_title()}
	loadingText={m.auth_verify_email_verifying()}
	successTitle={m.auth_verify_email_title()}
	successMessage={m.auth_verify_email_success()}
	primaryButtonLabel={isAuthenticated
		? m.auth_verify_email_btn_dashboard()
		: m.auth_verify_email_btn_login()}
	primaryButtonHref={isAuthenticated ? '/' : '/login'}
	action={performVerification}
/>
