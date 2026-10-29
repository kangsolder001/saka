<script lang="ts">
	import * as Table from '$lib/components/ui/table/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import RoleBadge from '$lib/components/app/RoleBadge.svelte';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import { resendInvitation, revokeInvitation } from '../api';
	import { formatRelative } from '$lib/format/date';
	import { errorMessage } from '$lib/api/error-message';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';
	import type { Invitation } from '$lib/api/types';

	interface Props {
		invitation: Invitation;
		orgId: string;
		onChanged?: () => void;
	}

	let { invitation, orgId, onChanged }: Props = $props();

	let confirmRevokeOpen = $state(false);
	let isRevoking = $state(false);
	let isResending = $state(false);

	let relativeExpiry = $derived(formatRelative(invitation.expires_at));

	async function handleResend() {
		isResending = true;
		try {
			await resendInvitation(orgId, invitation.id);
			toast.success(m.invitations_resend_success({ email: invitation.email }));
			onChanged?.();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			isResending = false;
		}
	}

	async function handleRevokeConfirm() {
		isRevoking = true;
		try {
			await revokeInvitation(orgId, invitation.id);
			toast.success(m.invitations_revoke_success());
			onChanged?.();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			isRevoking = false;
		}
	}
</script>

<Table.Row data-testid="invitation-row-{invitation.id}" class="hover:bg-muted/50">
	<Table.Cell class="py-3 font-medium text-foreground">
		<span data-testid="invitation-email">{invitation.email}</span>
	</Table.Cell>

	<Table.Cell class="py-3">
		<RoleBadge role={invitation.role} />
	</Table.Cell>

	<Table.Cell class="py-3 text-sm whitespace-nowrap text-muted-foreground">
		<span data-testid="invitation-expiry">{relativeExpiry}</span>
	</Table.Cell>

	<Table.Cell class="py-3 text-right">
		<div class="flex items-center justify-end gap-2">
			<Button
				variant="outline"
				class="min-h-[44px]"
				disabled={isResending}
				onclick={handleResend}
				data-testid="resend-invitation-btn-{invitation.id}"
			>
				{m.invitations_action_resend()}
			</Button>

			<Button
				variant="ghost"
				class="min-h-[44px] text-destructive hover:bg-destructive/10 hover:text-destructive"
				disabled={isRevoking}
				onclick={() => (confirmRevokeOpen = true)}
				data-testid="revoke-invitation-btn-{invitation.id}"
			>
				{m.invitations_action_revoke()}
			</Button>

			<ConfirmDialog
				bind:open={confirmRevokeOpen}
				title={m.invitations_action_revoke_confirm_title()}
				description={m.invitations_action_revoke_confirm_desc({ email: invitation.email })}
				confirmLabel={m.invitations_action_revoke()}
				variant="destructive"
				loading={isRevoking}
				onConfirm={handleRevokeConfirm}
			/>
		</div>
	</Table.Cell>
</Table.Row>
