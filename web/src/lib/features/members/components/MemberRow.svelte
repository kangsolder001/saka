<script lang="ts">
	import * as Table from '$lib/components/ui/table';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import UserAvatar from '$lib/components/app/UserAvatar.svelte';
	import RoleBadge from '$lib/components/app/RoleBadge.svelte';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import { can } from '$lib/features/org/permissions';
	import { changeMemberRole, removeMember } from '../api';
	import { formatDate } from '$lib/format/date';
	import { errorMessage } from '$lib/api/error-message';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages';
	import type { Member, Role } from '$lib/api/types';

	interface Props {
		member: Member;
		callerRole: Role;
		selfUserId: string;
		orgId: string;
		onChanged?: () => void;
	}

	let { member, callerRole, selfUserId, orgId, onChanged }: Props = $props();

	let confirmRemoveOpen = $state(false);
	let isRemoving = $state(false);
	let isChangingRole = $state(false);

	let isSelf = $derived(member.user_id === selfUserId);

	let canChangeRole = $derived(
		can(callerRole, 'member.changeRole', { userId: member.user_id, role: member.role }, selfUserId)
	);

	let canRemove = $derived(
		can(callerRole, 'member.remove', { userId: member.user_id, role: member.role }, selfUserId)
	);

	let joinedDate = $derived(
		formatDate((member as unknown as { created_at?: string }).created_at ?? member.joined_at)
	);

	async function handleRoleChange(newRole: string | undefined) {
		if (!newRole || (newRole !== 'admin' && newRole !== 'member')) return;
		if (newRole === member.role) return;

		isChangingRole = true;
		try {
			await changeMemberRole(orgId, member.user_id, newRole);
			toast.success(m.members_role_changed_success());
			onChanged?.();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			isChangingRole = false;
		}
	}

	async function handleRemoveConfirm() {
		isRemoving = true;
		try {
			await removeMember(orgId, member.user_id);
			toast.success(m.members_remove_success({ name: member.name }));
			onChanged?.();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			isRemoving = false;
		}
	}
</script>

<Table.Row data-testid="member-row-{member.user_id}" class="hover:bg-muted/50">
	<Table.Cell class="py-3">
		<div class="flex items-center gap-3">
			<UserAvatar
				name={member.name}
				email={member.email}
				avatarUrl={member.avatar_url}
				size="default"
			/>
			<div class="flex flex-col">
				<div class="flex items-center gap-2">
					<span class="font-medium text-foreground">{member.name}</span>
					{#if isSelf}
						<span
							data-testid="self-badge"
							class="rounded-full border border-border bg-muted/60 px-2 py-0.5 text-xs font-medium text-muted-foreground"
						>
							{m.members_self_label()}
						</span>
					{/if}
				</div>
				<span class="text-xs text-muted-foreground">{member.email}</span>
			</div>
		</div>
	</Table.Cell>

	<Table.Cell class="py-3">
		{#if canChangeRole}
			<Select.Root
				type="single"
				value={member.role}
				onValueChange={handleRoleChange}
				disabled={isChangingRole}
			>
				<Select.Trigger
					class="min-h-[44px] w-[130px]"
					data-testid="role-select-trigger-{member.user_id}"
				>
					{member.role === 'admin' ? m.badge_role_admin() : m.badge_role_member()}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="admin" label={m.badge_role_admin()} />
					<Select.Item value="member" label={m.badge_role_member()} />
				</Select.Content>
			</Select.Root>
		{:else}
			<RoleBadge role={member.role} />
		{/if}
	</Table.Cell>

	<Table.Cell class="py-3 text-sm whitespace-nowrap text-muted-foreground">
		{joinedDate}
	</Table.Cell>

	<Table.Cell class="py-3 text-right">
		{#if canRemove}
			<Button
				variant="ghost"
				class="min-h-[44px] text-destructive hover:bg-destructive/10 hover:text-destructive"
				onclick={() => (confirmRemoveOpen = true)}
				data-testid="remove-member-btn-{member.user_id}"
			>
				{m.members_action_remove()}
			</Button>

			<ConfirmDialog
				bind:open={confirmRemoveOpen}
				title={m.members_action_remove_confirm_title()}
				description={m.members_action_remove_confirm_desc({ name: member.name })}
				confirmLabel={m.members_action_remove()}
				variant="destructive"
				loading={isRemoving}
				onConfirm={handleRemoveConfirm}
			/>
		{/if}
	</Table.Cell>
</Table.Row>
