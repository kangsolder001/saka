<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { AlertTriangle, Building2, Clock, Loader2, XCircle } from 'lucide-svelte';
	import { session } from '$lib/auth/session.svelte';
	import { logout } from '$lib/auth/logout';
	import { ApiError } from '$lib/api/errors';
	import { errorMessage } from '$lib/api/error-message';
	import { formatDate } from '$lib/format/date';
	import {
		acceptInvitation,
		declineInvitation,
		getInvitation
	} from '$lib/features/invitations/api';
	import { resendVerification } from '$lib/features/auth/api';
	import type { InvitationInfo } from '$lib/api/types';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import RoleBadge from '$lib/components/app/RoleBadge.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';

	const token = $derived(page.url.searchParams.get('token') ?? '');

	type PageState = 'loading' | 'ready' | 'declined' | 'error';
	let pageState = $state<PageState>('loading');
	let errorText = $state<string | null>(null);
	let invitation = $state<InvitationInfo | null>(null);

	let accepting = $state(false);
	let declining = $state(false);
	let resending = $state(false);

	const isAuthenticated = $derived(session.status === 'authenticated');
	const currentUser = $derived(session.user);
	const isEmailMismatch = $derived(
		Boolean(
			isAuthenticated &&
			currentUser &&
			invitation &&
			currentUser.email.toLowerCase() !== invitation.email.toLowerCase()
		)
	);
	const isUnverified = $derived(isAuthenticated && currentUser?.email_verified === false);

	onMount(async () => {
		if (!token) {
			pageState = 'error';
			errorText = m.auth_invitation_not_found();
			return;
		}

		try {
			invitation = await getInvitation(token);
			pageState = 'ready';
		} catch (err: unknown) {
			pageState = 'error';
			if (err instanceof ApiError && err.status === 404) {
				errorText = err.reason ? errorMessage(err) : m.auth_invitation_not_found();
			} else {
				errorText = errorMessage(err);
			}
		}
	});

	async function handleAccept() {
		if (accepting || !token) return;
		accepting = true;
		try {
			const res = await acceptInvitation(token);
			toast.success(m.auth_invitation_accepted_toast({ organization: res.organization.name }));
			await goto(`/orgs/${res.organization.id}`);
		} catch (err: unknown) {
			toast.error(errorMessage(err));
		} finally {
			accepting = false;
		}
	}

	async function handleDecline() {
		if (declining || !token) return;
		declining = true;
		try {
			await declineInvitation(token);
			pageState = 'declined';
		} catch (err: unknown) {
			toast.error(errorMessage(err));
		} finally {
			declining = false;
		}
	}

	async function handleResendVerification() {
		if (resending) return;
		resending = true;
		try {
			await resendVerification();
			toast.success(m.banner_resend_success());
		} catch (err: unknown) {
			toast.error(errorMessage(err));
		} finally {
			resending = false;
		}
	}

	async function handleSwitchAccount() {
		await logout();
	}
</script>

<AuthCard title={m.auth_invitation_title()}>
	{#if pageState === 'loading'}
		<div class="flex flex-col items-center justify-center py-6 text-center">
			<Loader2 class="mb-4 size-8 animate-spin text-primary" />
			<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
		</div>
	{:else if pageState === 'declined'}
		<div class="flex flex-col items-center justify-center py-6 text-center">
			<div
				class="mb-4 flex size-14 items-center justify-center rounded-full bg-muted text-muted-foreground"
			>
				<XCircle class="size-7" />
			</div>
			<p class="text-sm text-muted-foreground">{m.auth_invitation_declined_msg()}</p>
		</div>
		<Button href="/" variant="outline" class="h-11 w-full">
			{m.org_back_to_list()}
		</Button>
	{:else if pageState === 'error'}
		<div class="flex flex-col items-center justify-center py-6 text-center">
			<div
				class="mb-4 flex size-14 items-center justify-center rounded-full bg-danger-muted text-destructive"
			>
				<AlertTriangle class="size-7" />
			</div>
			<p class="text-sm text-destructive" role="alert">
				{errorText || m.auth_invitation_not_found()}
			</p>
		</div>
		<Button href="/" variant="outline" class="h-11 w-full">
			{m.org_back_to_list()}
		</Button>
	{:else if invitation}
		<div class="space-y-4">
			<!-- Organization & Inviter Summary Box -->
			<div
				class="flex flex-col items-center rounded-xl border border-border/80 bg-muted/40 p-5 text-center"
			>
				{#if invitation.organization.logo_url}
					<img
						src={invitation.organization.logo_url}
						alt={invitation.organization.name}
						class="mb-3 size-14 rounded-full border border-border object-cover"
					/>
				{:else}
					<div
						class="mb-3 flex size-14 items-center justify-center rounded-full bg-primary/10 text-primary"
					>
						<Building2 class="size-7" />
					</div>
				{/if}

				<h3 class="text-lg font-bold text-foreground">
					{invitation.organization.name}
				</h3>

				<div class="mt-2 flex items-center gap-2">
					<RoleBadge role={invitation.role} />
				</div>

				<p class="mt-3 text-xs text-muted-foreground">
					{m.auth_invitation_desc({
						inviter: invitation.inviter_name,
						organization: invitation.organization.name,
						role: invitation.role
					})}
				</p>

				<div class="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
					<Clock class="size-3.5" />
					<span>{m.auth_invitation_expires_at({ date: formatDate(invitation.expires_at) })}</span>
				</div>
			</div>

			<!-- Warnings / Action Branches -->
			{#if isEmailMismatch}
				<div
					class="rounded-lg border border-warning/30 bg-warning-muted p-4 text-xs text-warning"
					role="alert"
				>
					<p>
						{m.auth_invitation_different_email_warn({
							email: invitation.email,
							currentEmail: currentUser?.email ?? ''
						})}
					</p>
					<Button
						variant="outline"
						size="sm"
						onclick={handleSwitchAccount}
						class="mt-3 h-8 w-full border-warning/40 text-foreground"
					>
						{m.auth_invitation_switch_account()}
					</Button>
				</div>
			{:else if isUnverified}
				<div
					class="rounded-lg border border-warning/30 bg-warning-muted p-4 text-xs text-warning"
					role="alert"
				>
					<p>{m.auth_invitation_unverified_warn()}</p>
					<Button
						variant="outline"
						size="sm"
						disabled={resending}
						onclick={handleResendVerification}
						class="mt-3 h-8 w-full border-warning/40 text-foreground"
					>
						{resending ? m.banner_resending() : m.auth_token_resend_btn()}
					</Button>
				</div>
			{:else if isAuthenticated}
				<div class="flex flex-col gap-2 pt-2">
					<Button class="h-11 w-full" disabled={accepting || declining} onclick={handleAccept}>
						{#if accepting}
							<Loader2 class="mr-2 size-4 animate-spin" />
							{m.common_loading()}
						{:else}
							{m.auth_invitation_accept_btn()}
						{/if}
					</Button>

					<Button
						variant="outline"
						class="h-11 w-full"
						disabled={accepting || declining}
						onclick={handleDecline}
					>
						{#if declining}
							<Loader2 class="mr-2 size-4 animate-spin" />
							{m.common_loading()}
						{:else}
							{m.auth_invitation_decline_btn()}
						{/if}
					</Button>
				</div>
			{:else}
				<!-- Not logged in -->
				<div class="flex flex-col gap-2 pt-2">
					{#if invitation.account_exists}
						<Button
							href="/login?redirect={encodeURIComponent(`/accept-invitation?token=${token}`)}"
							class="h-11 w-full"
						>
							{m.auth_invitation_login_to_accept()}
						</Button>
					{:else}
						<Button href="/register?invitation={encodeURIComponent(token)}" class="h-11 w-full">
							{m.auth_invitation_register_to_accept()}
						</Button>
					{/if}

					<Button
						variant="outline"
						class="h-11 w-full"
						disabled={declining}
						onclick={handleDecline}
					>
						{#if declining}
							<Loader2 class="mr-2 size-4 animate-spin" />
							{m.common_loading()}
						{:else}
							{m.auth_invitation_decline_btn()}
						{/if}
					</Button>
				</div>
			{/if}
		</div>
	{/if}
</AuthCard>
