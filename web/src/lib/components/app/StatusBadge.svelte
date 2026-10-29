<script lang="ts">
	import * as m from '$lib/paraglide/messages.js';
	import { cn } from '$lib/utils.js';

	type StatusKind = 'subscription' | 'invoice';

	interface Props {
		kind: StatusKind;
		status: string;
		class?: string;
	}

	let { kind, status, class: className }: Props = $props();

	type BadgeTone = 'success' | 'info' | 'warning' | 'destructive' | 'neutral';

	const toneStyles: Record<BadgeTone, string> = {
		success: 'bg-success-muted text-success border-success/30',
		info: 'bg-info-muted text-info border-info/30',
		warning: 'bg-warning-muted text-warning border-warning/30',
		destructive: 'bg-danger-muted text-destructive border-destructive/30',
		neutral: 'bg-muted text-muted-foreground border-border'
	};

	let tone = $derived.by<BadgeTone>(() => {
		switch (status) {
			case 'active':
			case 'paid':
				return 'success';
			case 'trialing':
			case 'open':
				return 'info';
			case 'past_due':
			case 'canceling':
				return 'warning';
			case 'uncollectible':
				return 'destructive';
			case 'canceled':
			case 'expired':
			case 'draft':
			case 'void':
			default:
				return 'neutral';
		}
	});

	let label = $derived.by<string>(() => {
		switch (status) {
			case 'active':
				return m.badge_status_active();
			case 'trialing':
				return m.badge_status_trialing();
			case 'past_due':
				return m.badge_status_past_due();
			case 'canceled':
				return m.badge_status_canceled();
			case 'expired':
				return m.badge_status_expired();
			case 'paid':
				return m.badge_status_paid();
			case 'open':
				return m.badge_status_open();
			case 'draft':
				return m.badge_status_draft();
			case 'void':
				return m.badge_status_void();
			case 'canceling':
				return m.badge_status_canceling();
			case 'uncollectible':
				return m.badge_status_uncollectible();
			default:
				return status;
		}
	});
</script>

<span
	data-slot="status-badge"
	data-kind={kind}
	data-status={status}
	class={cn(
		'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium transition-colors',
		toneStyles[tone],
		className
	)}
>
	{label}
</span>
