<script lang="ts">
	import { goto } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { Loader2 } from 'lucide-svelte';
	import { toast } from 'svelte-sonner';
	import { ApiError } from '$lib/api/errors';
	import { queryClient as fallbackQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import AppShell from '$lib/components/app/AppShell.svelte';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { createOrganization, setLastOrgId } from '$lib/features/org/api';
	import { createOrgSchema } from '$lib/features/org/schemas';
	import { createForm } from '$lib/forms/form.svelte';
	import * as m from '$lib/paraglide/messages';

	let queryClient: ReturnType<typeof useQueryClient>;
	try {
		queryClient = useQueryClient();
	} catch {
		queryClient = fallbackQueryClient;
	}

	const form = createForm({
		schema: createOrgSchema,
		initial: {
			name: '',
			slug: ''
		},
		onSubmit: async (values) => {
			try {
				const slug = values.slug?.trim() || undefined;
				const res = await createOrganization({
					name: values.name.trim(),
					slug
				});
				setLastOrgId(res.organization.id);
				await queryClient.invalidateQueries({ queryKey: queryKeys.me });
				toast.success(m.org_create_success());
				await goto(`/orgs/${res.organization.id}`);
			} catch (err: unknown) {
				if (err instanceof ApiError && err.status === 409) {
					form.errors.slug = err.serverMessage || 'Slug ini sudah dipakai';
					return;
				}
				if (err instanceof ApiError && err.status === 429) {
					if (err.code !== 'RATE_LIMITED') {
						err.code = 'RATE_LIMITED';
					}
				}
				throw err;
			}
		}
	});
</script>

<AppShell>
	<div class="mx-auto flex w-full max-w-2xl flex-col gap-6">
		<PageHeader
			title={m.org_new_title()}
			description={m.org_new_desc ? m.org_new_desc() : undefined}
		/>

		<Card.Root class="border-border">
			<form onsubmit={form.submit}>
				<Card.Content class="space-y-6 pt-6">
					{#if form.formError}
						<div
							class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
							role="alert"
							data-testid="form-error"
						>
							{form.formError}
						</div>
					{/if}

					<div class="space-y-2">
						<Label for="name">{m.org_field_name()}</Label>
						<Input
							id="name"
							name="name"
							type="text"
							required
							placeholder={m.org_field_name_placeholder()}
							bind:value={form.values.name}
							class="h-10 {form.errors.name
								? 'border-destructive focus-visible:ring-destructive'
								: ''}"
							disabled={form.submitting}
							data-testid="org-name-input"
						/>
						{#if form.errors.name}
							<p class="text-xs text-destructive" data-testid="org-name-error">
								{form.errors.name}
							</p>
						{/if}
					</div>

					<div class="space-y-2">
						<Label for="slug">{m.org_field_slug()}</Label>
						<Input
							id="slug"
							name="slug"
							type="text"
							placeholder={m.org_field_slug_placeholder()}
							bind:value={form.values.slug}
							class="h-10 {form.errors.slug
								? 'border-destructive focus-visible:ring-destructive'
								: ''}"
							disabled={form.submitting}
							data-testid="org-slug-input"
						/>
						<p class="text-xs text-muted-foreground">{m.org_field_slug_help()}</p>
						{#if form.errors.slug}
							<p class="text-xs text-destructive" data-testid="org-slug-error">
								{form.errors.slug}
							</p>
						{/if}
					</div>
				</Card.Content>

				<Card.Footer class="flex items-center justify-between border-t border-border pt-4">
					<Button
						variant="ghost"
						href="/orgs"
						disabled={form.submitting}
						class="min-h-[44px]"
						data-testid="cancel-btn"
					>
						{m.common_cancel ? m.common_cancel() : 'Cancel'}
					</Button>
					<Button
						type="submit"
						disabled={form.submitting}
						class="min-h-[44px] gap-2"
						data-testid="submit-org-btn"
					>
						{#if form.submitting}
							<Loader2 class="size-4 animate-spin" aria-hidden="true" />
							<span>{m.org_create_submitting()}</span>
						{:else}
							<span>{m.org_create_submit()}</span>
						{/if}
					</Button>
				</Card.Footer>
			</form>
		</Card.Root>
	</div>
</AppShell>
