<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { Globe, Laptop, Smartphone } from 'lucide-svelte';
	import { queryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import { logout } from '$lib/auth/logout';
	import { createSessionsQuery, revokeSession } from '$lib/features/account/api';
	import { describeUserAgent } from '$lib/features/account/user-agent';
	import { formatDateTime } from '$lib/format/date';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';
	import type { Session } from '$lib/api/types';

	const sessionsQuery = createSessionsQuery();

	let sessionToRevoke = $state<Session | null>(null);
	let confirmDialogOpen = $state(false);
	let revoking = $state(false);

	function openRevokeConfirm(session: Session) {
		sessionToRevoke = session;
		confirmDialogOpen = true;
	}

	async function handleRevokeConfirm() {
		if (!sessionToRevoke) return;
		revoking = true;
		const targetSession = sessionToRevoke;

		try {
			await revokeSession(targetSession.id);
			confirmDialogOpen = false;

			if (targetSession.current) {
				await logout();
			} else {
				await queryClient.invalidateQueries({ queryKey: queryKeys.sessions });
				toast.success(m.account_session_revoked_success());
			}
		} catch (err: unknown) {
			toast.error(err instanceof Error ? err.message : 'Failed to revoke session');
		} finally {
			revoking = false;
			sessionToRevoke = null;
		}
	}
</script>

<Card.Root class="border-border">
	<Card.Header>
		<Card.Title>{m.account_sessions_title()}</Card.Title>
		<Card.Description>{m.account_sessions_desc()}</Card.Description>
	</Card.Header>

	<Card.Content>
		{#if sessionsQuery.isPending}
			<PageSkeleton />
		{:else if sessionsQuery.isError}
			<ErrorState error={sessionsQuery.error} retry={() => sessionsQuery.refetch()} />
		{:else if sessionsQuery.data}
			<div class="divide-y divide-border">
				{#each sessionsQuery.data as session (session.id)}
					{@const ua = describeUserAgent(session.user_agent)}
					<div
						class="flex flex-col gap-4 py-4 sm:flex-row sm:items-center sm:justify-between"
						data-testid="session-row-{session.id}"
					>
						<div class="flex items-start gap-3">
							<div
								class="mt-1 flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50 text-muted-foreground"
							>
								{#if ua.os === 'iOS' || ua.os === 'Android'}
									<Smartphone class="size-4" />
								{:else if ua.os === 'macOS' || ua.os === 'Windows' || ua.os === 'Linux'}
									<Laptop class="size-4" />
								{:else}
									<Globe class="size-4" />
								{/if}
							</div>

							<div class="space-y-1">
								<div class="flex flex-wrap items-center gap-2">
									<p class="font-medium text-foreground">{ua.formatted}</p>
									{#if session.current}
										<Badge variant="secondary" class="text-xs font-medium">
											{m.account_session_current()}
										</Badge>
									{/if}
								</div>

								<div
									class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground"
								>
									<span>{session.ip}</span>
									<span>•</span>
									<span>
										{m.account_session_last_active()}: {formatDateTime(session.last_used_at)}
									</span>
								</div>
							</div>
						</div>

						<div class="flex shrink-0 items-center">
							<Button
								variant="outline"
								size="sm"
								onclick={() => openRevokeConfirm(session)}
								class="min-h-[36px] text-destructive hover:bg-destructive/10 hover:text-destructive"
							>
								{m.account_session_revoke()}
							</Button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</Card.Content>
</Card.Root>

<ConfirmDialog
	bind:open={confirmDialogOpen}
	title={m.account_session_revoke_confirm_title()}
	description={m.account_session_revoke_confirm_desc()}
	confirmLabel={m.account_session_revoke()}
	variant="destructive"
	loading={revoking}
	onConfirm={handleRevokeConfirm}
	onCancel={() => {
		confirmDialogOpen = false;
		sessionToRevoke = null;
	}}
/>
