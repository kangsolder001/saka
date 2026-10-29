<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { Loader2 } from 'lucide-svelte';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import { createForm } from '$lib/forms/form.svelte';
	import { createMembersQuery, transferOwnership } from '$lib/features/org/api';
	import { transferOwnershipSchema } from '$lib/features/org/schemas';
	import { can } from '$lib/features/org/permissions';
	import type { Member, Role } from '$lib/api/types';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		orgId: string;
		members?: Member[];
		selfUserId: string;
		role?: Role;
	}

	let { orgId, members: membersProp, selfUserId, role }: Props = $props();

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	const membersQuery = createMembersQuery(() => orgId);

	let membersList = $derived(membersProp ?? membersQuery.data ?? []);
	let eligibleMembers = $derived(
		membersList.filter((item) => item.user_id !== selfUserId && item.role !== 'owner')
	);

	const form = createForm({
		schema: transferOwnershipSchema,
		initial: {
			user_id: '',
			current_password: ''
		},
		onSubmit: async (values) => {
			await transferOwnership(orgId, {
				user_id: values.user_id,
				current_password: values.current_password
			});

			await qc.invalidateQueries({ queryKey: queryKeys.organization(orgId) });
			await qc.invalidateQueries({ queryKey: queryKeys.me });

			const target = membersList.find((item) => item.user_id === values.user_id);
			const targetName = target?.name || target?.email || '';

			toast.success(m.org_transfer_success({ name: targetName }));
			form.reset();
		}
	});
</script>

{#if !role || can(role, 'ownership.transfer')}
	<Card.Root class="border-border shadow-none">
		<Card.Header>
			<Card.Title>{m.org_transfer_title()}</Card.Title>
			<Card.Description>{m.org_transfer_desc()}</Card.Description>
		</Card.Header>

		<form onsubmit={form.submit}>
			<Card.Content class="space-y-4">
				{#if form.formError}
					<div
						class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
						role="alert"
					>
						{form.formError}
					</div>
				{/if}

				<!-- Member Select -->
				<div class="space-y-2">
					<Label for="transfer-member">{m.org_transfer_select_member()}</Label>
					<select
						id="transfer-member"
						name="user_id"
						bind:value={form.values.user_id}
						disabled={form.submitting || eligibleMembers.length === 0}
						required
						class="flex h-10 min-h-[44px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
					>
						<option value="" disabled selected={!form.values.user_id}>
							{m.org_transfer_select_member()}
						</option>
						{#each eligibleMembers as member (member.user_id)}
							<option value={member.user_id}>
								{member.name || member.email} ({member.email})
							</option>
						{/each}
					</select>
					{#if form.errors.user_id}
						<p class="text-xs text-destructive">{form.errors.user_id}</p>
					{/if}
				</div>

				<!-- Current Password -->
				<div class="space-y-2">
					<Label for="transfer-password">{m.account_current_password()}</Label>
					<Input
						id="transfer-password"
						name="current_password"
						type="password"
						required
						autocomplete="current-password"
						disabled={form.submitting}
						bind:value={form.values.current_password}
						class="h-10 min-h-[44px]"
					/>
					{#if form.errors.current_password}
						<p class="text-xs text-destructive">{form.errors.current_password}</p>
					{/if}
				</div>
			</Card.Content>

			<Card.Footer class="flex justify-end border-t border-border pt-4">
				<Button
					type="submit"
					variant="destructive"
					disabled={form.submitting ||
						!form.values.user_id ||
						!form.values.current_password ||
						eligibleMembers.length === 0}
					class="min-h-[44px]"
				>
					{#if form.submitting}
						<Loader2 class="mr-2 size-4 animate-spin" />
						{m.org_transfer_submitting()}
					{:else}
						{m.org_transfer_submit()}
					{/if}
				</Button>
			</Card.Footer>
		</form>
	</Card.Root>
{/if}
