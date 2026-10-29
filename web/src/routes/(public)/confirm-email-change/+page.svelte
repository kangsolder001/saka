<script lang="ts">
	import { page } from '$app/state';
	import { session } from '$lib/auth/session.svelte';
	import { ApiError } from '$lib/api/errors';
	import { confirmEmailChange } from '$lib/features/auth/api';
	import TokenActionPage from '$lib/features/auth/components/TokenActionPage.svelte';
	import * as m from '$lib/paraglide/messages';

	const token = $derived(page.url.searchParams.get('token') ?? '');
	const isAuthenticated = $derived(session.status === 'authenticated');

	async function performConfirmation() {
		if (!token) {
			throw new Error(m.field_error_token_invalid());
		}
		const res = await confirmEmailChange(token);
		if (session.user) {
			session.user.email = res.email;
			session.user.email_verified = res.email_verified;
		}
	}

	function handleError(err: unknown): string | null {
		if (err instanceof ApiError && err.status === 409) {
			return m.auth_confirm_email_conflict();
		}
		return null;
	}
</script>

<TokenActionPage
	title={m.auth_confirm_email_title()}
	loadingText={m.auth_confirm_email_verifying()}
	successTitle={m.auth_confirm_email_title()}
	successMessage={m.auth_confirm_email_success()}
	primaryButtonLabel={isAuthenticated
		? m.auth_verify_email_btn_dashboard()
		: m.auth_verify_email_btn_login()}
	primaryButtonHref={isAuthenticated ? '/' : '/login'}
	action={performConfirmation}
	errorOverride={handleError}
/>
