<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { Loader2 } from 'lucide-svelte';
	import { queryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import { session } from '$lib/auth/session.svelte';
	import { createForm } from '$lib/forms/form.svelte';
	import { createMeQuery, updateMe, type UpdateMeParams } from '$lib/features/account/api';
	import { profileSchema } from '$lib/features/account/schemas';
	import UserAvatar from '$lib/components/app/UserAvatar.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	const meQuery = createMeQuery();

	let initialName = $state('');
	let initialAvatarUrl = $state('');
	let isInitialized = $state(false);

	const form = createForm({
		schema: profileSchema,
		initial: {
			name: '',
			avatar_url: ''
		},
		onSubmit: async (values) => {
			const patch: UpdateMeParams = {};
			if (values.name !== initialName) {
				patch.name = values.name;
			}
			if (values.avatar_url !== initialAvatarUrl) {
				patch.avatar_url = values.avatar_url ? values.avatar_url : null;
			}

			// If nothing changed, do not call API
			if (Object.keys(patch).length === 0) {
				return;
			}

			await updateMe(patch);
			await queryClient.invalidateQueries({ queryKey: queryKeys.me });

			initialName = values.name;
			initialAvatarUrl = values.avatar_url ?? '';

			if (session.user && patch.name) {
				session.user.name = patch.name;
			}

			toast.success(m.account_profile_success());
		}
	});

	$effect(() => {
		if (meQuery.data?.user && !isInitialized) {
			const u = meQuery.data.user;
			initialName = u.name || '';
			initialAvatarUrl = u.avatar_url || '';
			form.values.name = initialName;
			form.values.avatar_url = initialAvatarUrl;
			isInitialized = true;
		}
	});

	const isDirty = $derived(
		form.values.name !== initialName || (form.values.avatar_url ?? '') !== initialAvatarUrl
	);
</script>

{#if meQuery.isPending && !isInitialized}
	<PageSkeleton />
{:else if meQuery.isError}
	<ErrorState error={meQuery.error} retry={() => meQuery.refetch()} />
{:else if meQuery.data?.user}
	{@const user = meQuery.data.user}
	<Card.Root class="border-border">
		<Card.Header>
			<Card.Title>{m.account_profile_title()}</Card.Title>
			<Card.Description>{m.account_profile_desc()}</Card.Description>
		</Card.Header>

		<form onsubmit={form.submit}>
			<Card.Content class="space-y-6">
				{#if form.formError}
					<div
						class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
						role="alert"
					>
						{form.formError}
					</div>
				{/if}

				<!-- Avatar Preview & URL -->
				<div class="flex flex-col gap-4 sm:flex-row sm:items-center">
					<div class="shrink-0">
						<UserAvatar
							name={form.values.name || user.name}
							email={user.email}
							avatarUrl={form.values.avatar_url}
							size="xl"
						/>
					</div>
					<div class="flex-1 space-y-2">
						<Label for="avatar_url">{m.account_profile_avatar_url()}</Label>
						<Input
							id="avatar_url"
							name="avatar_url"
							type="url"
							placeholder="https://example.com/avatar.jpg"
							bind:value={form.values.avatar_url}
							class="h-10"
						/>
						<p class="text-xs text-muted-foreground">{m.account_profile_avatar_help()}</p>
						{#if form.errors.avatar_url}
							<p class="text-xs text-destructive">{form.errors.avatar_url}</p>
						{/if}
					</div>
				</div>

				<!-- Full Name -->
				<div class="space-y-2">
					<Label for="name">{m.account_profile_name()}</Label>
					<Input
						id="name"
						name="name"
						type="text"
						required
						bind:value={form.values.name}
						class="h-10"
					/>
					{#if form.errors.name}
						<p class="text-xs text-destructive">{form.errors.name}</p>
					{/if}
				</div>

				<!-- Email (Read-only with reference to Security tab) -->
				<div class="space-y-2">
					<Label for="email">{m.account_profile_email()}</Label>
					<Input
						id="email"
						name="email"
						type="email"
						value={user.email}
						disabled
						class="h-10 cursor-not-allowed bg-muted opacity-80"
					/>
					<div class="flex items-center gap-1.5 text-xs text-muted-foreground">
						<span>{m.account_profile_email_help()}</span>
						<a
							href="/account/security"
							class="font-medium text-primary underline underline-offset-4 hover:text-primary/80"
						>
							{m.account_tab_security()}
						</a>
					</div>
				</div>
			</Card.Content>

			<Card.Footer class="flex justify-end border-t border-border pt-4">
				<Button
					type="submit"
					disabled={form.submitting || !isDirty}
					class="min-h-[44px] min-w-[120px]"
				>
					{#if form.submitting}
						<Loader2 class="mr-2 size-4 animate-spin" />
					{/if}
					{m.account_profile_save()}
				</Button>
			</Card.Footer>
		</form>
	</Card.Root>
{/if}
