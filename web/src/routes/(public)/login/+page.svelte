<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { Loader2 } from 'lucide-svelte';
	import { session } from '$lib/auth/session.svelte';
	import { safeRedirect } from '$lib/auth/guards';
	import { ApiError } from '$lib/api/errors';
	import { createForm } from '$lib/forms/form.svelte';
	import { loginSchema } from '$lib/features/auth/schemas';
	import { login } from '$lib/features/auth/api';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as m from '$lib/paraglide/messages';

	$effect(() => {
		if (session.status === 'authenticated') {
			const redirectUrl = safeRedirect(page.url.searchParams.get('redirect'), '/');
			void goto(redirectUrl);
		}
	});

	const form = createForm({
		schema: loginSchema,
		initial: { email: '', password: '' },
		onSubmit: async (values) => {
			try {
				await login(values);
				const redirectUrl = safeRedirect(page.url.searchParams.get('redirect'), '/');
				await goto(redirectUrl);
			} catch (err) {
				if (err instanceof ApiError) {
					if (err.status === 401) {
						throw new Error(m.auth_login_error_invalid(), { cause: err });
					}
					if (
						err.status === 403 &&
						typeof err.reason === 'string' &&
						err.reason.startsWith('account_')
					) {
						throw new Error(m.auth_login_error_account_disabled(), { cause: err });
					}
				}
				throw err;
			}
		}
	});
</script>

<AuthCard title={m.auth_login_title()} description={m.auth_login_desc()}>
	<form onsubmit={form.submit} class="space-y-4">
		{#if form.formError}
			<div
				class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
				role="alert"
			>
				{form.formError}
			</div>
		{/if}

		<div class="space-y-2">
			<Label for="email">{m.auth_email()}</Label>
			<Input
				id="email"
				name="email"
				type="email"
				autocomplete="email"
				placeholder="name@example.com"
				bind:value={form.values.email}
				class="h-11 {form.errors.email ? 'border-destructive focus-visible:ring-destructive' : ''}"
				disabled={form.submitting}
			/>
			{#if form.errors.email}
				<p class="text-xs text-destructive" role="alert">{form.errors.email}</p>
			{/if}
		</div>

		<div class="space-y-2">
			<div class="flex items-center justify-between">
				<Label for="password">{m.auth_password()}</Label>
				<a
					href="/forgot-password"
					class="text-xs font-medium text-primary hover:underline"
					tabindex={form.submitting ? -1 : 0}
				>
					{m.auth_login_forgot_password()}
				</a>
			</div>
			<Input
				id="password"
				name="password"
				type="password"
				autocomplete="current-password"
				bind:value={form.values.password}
				class="h-11 {form.errors.password
					? 'border-destructive focus-visible:ring-destructive'
					: ''}"
				disabled={form.submitting}
			/>
			{#if form.errors.password}
				<p class="text-xs text-destructive" role="alert">{form.errors.password}</p>
			{/if}
		</div>

		<Button type="submit" class="h-11 w-full" disabled={form.submitting}>
			{#if form.submitting}
				<Loader2 class="mr-2 size-4 animate-spin" />
				{m.auth_login_submitting()}
			{:else}
				{m.auth_login_submit()}
			{/if}
		</Button>
	</form>

	{#snippet footer()}
		<p class="text-xs text-muted-foreground">
			{m.auth_login_no_account()}
			<a href="/register" class="font-medium text-primary hover:underline">
				{m.auth_login_register_link()}
			</a>
		</p>
	{/snippet}
</AuthCard>
