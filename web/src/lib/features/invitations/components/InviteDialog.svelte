<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { createInvitation } from '../api';
	import { showUpgrade } from '$lib/features/billing/upgrade.svelte';
	import { queryKeys } from '$lib/api/query-keys';
	import { ApiError } from '$lib/api/errors';
	import { errorMessage } from '$lib/api/error-message';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import * as m from '$lib/paraglide/messages.js';

	interface Props {
		open?: boolean;
		orgId: string;
		onSuccess?: () => void;
	}

	let { open = $bindable(false), orgId, onSuccess }: Props = $props();

	const queryClient = useQueryClient();

	let email = $state('');
	let role = $state<'admin' | 'member'>('member');
	let submitting = $state(false);
	let emailError = $state<string | null>(null);

	function handleOpenChange(nextOpen: boolean) {
		open = nextOpen;
		if (!nextOpen) {
			email = '';
			role = 'member';
			emailError = null;
			submitting = false;
		}
	}

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		const trimmedEmail = email.trim();
		if (!trimmedEmail || submitting) return;

		submitting = true;
		emailError = null;

		try {
			const res = await createInvitation(orgId, { email: trimmedEmail, role });
			if (res.status === 200) {
				toast.success(m.invite_success_refresh());
			} else {
				toast.success(m.invite_success_new());
			}

			await Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.invitations(orgId) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.subscription(orgId) })
			]);

			open = false;
			email = '';
			role = 'member';
			onSuccess?.();
		} catch (err: unknown) {
			if (err instanceof ApiError) {
				if (
					err.status === 402 ||
					err.code === 'PLAN_LIMIT_REACHED' ||
					err.code === 'PLAN_FEATURE_UNAVAILABLE'
				) {
					open = false;
					showUpgrade(err, orgId);
					return;
				}
				if (err.status === 409 || err.code === 'CONFLICT') {
					emailError = m.invite_error_already_member();
					return;
				}
			}
			toast.error(errorMessage(err));
		} finally {
			submitting = false;
		}
	}
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
	<Dialog.Content class="sm:max-w-[425px]" data-testid="invite-dialog">
		<Dialog.Header>
			<Dialog.Title>{m.invite_dialog_title()}</Dialog.Title>
			<Dialog.Description>{m.invite_dialog_desc()}</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={handleSubmit} class="space-y-4 py-2">
			<div class="space-y-2">
				<Label for="invite-email">{m.invite_field_email()}</Label>
				<Input
					id="invite-email"
					type="email"
					bind:value={email}
					placeholder="colleague@example.com"
					required
					disabled={submitting}
					class="min-h-[44px]"
					data-testid="invite-email-input"
				/>
				{#if emailError}
					<p class="text-xs text-destructive" data-testid="invite-email-error">
						{emailError}
					</p>
				{/if}
			</div>

			<div class="space-y-2">
				<Label for="invite-role">{m.invite_field_role()}</Label>
				<Select.Root type="single" bind:value={role} disabled={submitting}>
					<Select.Trigger
						id="invite-role"
						class="min-h-[44px] w-full"
						data-testid="invite-role-trigger"
					>
						{role === 'admin' ? m.badge_role_admin() : m.badge_role_member()}
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="member" label={m.badge_role_member()} />
						<Select.Item value="admin" label={m.badge_role_admin()} />
					</Select.Content>
				</Select.Root>
			</div>

			<Dialog.Footer class="pt-4">
				<Button
					type="button"
					variant="outline"
					class="min-h-[44px]"
					disabled={submitting}
					onclick={() => (open = false)}
				>
					{m.common_cancel ? m.common_cancel() : 'Cancel'}
				</Button>
				<Button
					type="submit"
					class="min-h-[44px]"
					disabled={submitting || !email.trim()}
					data-testid="invite-submit-btn"
				>
					{submitting ? m.invite_submitting() : m.invite_submit()}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
