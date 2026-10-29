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
		success:
			'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-950/50 dark:text-emerald-300 dark:border-emerald-800',
		info: 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-950/50 dark:text-blue-300 dark:border-blue-800',
		warning:
			'bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-950/50 dark:text-amber-300 dark:border-amber-800',
		destructive:
			'bg-red-50 text-red-700 border-red-200 dark:bg-red-950/50 dark:text-red-300 dark:border-red-800',
		neutral:
			'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700'
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
