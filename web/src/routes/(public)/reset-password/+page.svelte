<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { Loader2 } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';
	import { createForm } from '$lib/forms/form.svelte';
	import { resetPasswordSchema } from '$lib/features/auth/schemas';
	import { resetPassword } from '$lib/features/auth/api';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as m from '$lib/paraglide/messages';

	const token = $derived(page.url.searchParams.get('token'));

	const form = createForm({
		schema: resetPasswordSchema,
		initial: { password: '', confirmPassword: '' },
		onSubmit: async (values) => {
			if (!token) {
				throw new Error(m.field_error_token_invalid());
			}
			await resetPassword(token, values.password);
			toast.success(m.auth_reset_password_success_toast());
			await goto('/login');
		}
	});
</script>

<AuthCard title={m.auth_reset_password_title()} description={m.auth_reset_password_desc()}>
	{#if !token}
		<div
			class="rounded-md border border-destructive/20 bg-danger-muted p-4 text-center text-sm text-destructive"
			role="alert"
		>
			<p>{m.field_error_token_invalid()}</p>
			<Button href="/forgot-password" variant="outline" size="sm" class="mt-3">
				{m.auth_forgot_password_title()}
			</Button>
		</div>
	{:else}
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
				<Label for="password">{m.auth_password()}</Label>
				<Input
					id="password"
					name="password"
					type="password"
					autocomplete="new-password"
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

			<div class="space-y-2">
				<Label for="confirmPassword">{m.auth_password_confirm()}</Label>
				<Input
					id="confirmPassword"
					name="confirmPassword"
					type="password"
					autocomplete="new-password"
					bind:value={form.values.confirmPassword}
					class="h-11 {form.errors.confirmPassword
						? 'border-destructive focus-visible:ring-destructive'
						: ''}"
					disabled={form.submitting}
				/>
				{#if form.errors.confirmPassword}
					<p class="text-xs text-destructive" role="alert">{form.errors.confirmPassword}</p>
				{/if}
			</div>

			<Button type="submit" class="h-11 w-full" disabled={form.submitting}>
				{#if form.submitting}
					<Loader2 class="mr-2 size-4 animate-spin" />
					{m.auth_reset_password_submitting()}
				{:else}
					{m.auth_reset_password_submit()}
				{/if}
			</Button>
		</form>
	{/if}

	{#snippet footer()}
		<a href="/login" class="text-xs font-medium text-primary hover:underline">
			{m.auth_login_submit()}
		</a>
	{/snippet}
</AuthCard>
