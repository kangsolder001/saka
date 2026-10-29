import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import UsageMeter from './UsageMeter.svelte';
import { setLocale } from '$lib/i18n';

describe('UsageMeter.svelte', () => {
	it('renders standard state (< 80%) with bg-primary', async () => {
		setLocale('id');
		const screen = render(UsageMeter, {
			label: 'Anggota',
			used: 5,
			max: 10,
			unit: 'anggota'
		});

		expect(screen.getByTestId('usage-meter-label').element().textContent).toBe('Anggota');
		expect(screen.getByTestId('usage-meter-percentage').element().textContent?.trim()).toBe('50%');
		expect(screen.getByTestId('usage-meter-count').element().textContent).toContain(
			'5 / 10 anggota'
		);

		const fill = screen.getByTestId('usage-meter-fill');
		expect(fill.element().classList.contains('bg-primary')).toBe(true);
		expect(fill.element().style.width).toBe('50%');

		const progress = screen.getByRole('progressbar');
		expect(progress.element().getAttribute('aria-valuenow')).toBe('5');
		expect(progress.element().getAttribute('aria-valuemax')).toBe('10');
	});

	it('renders warning state (80% <= used < 100%) with bg-warning and text-warning', async () => {
		const screen = render(UsageMeter, {
			label: 'Storage',
			used: 85,
			max: 100
		});

		expect(screen.getByTestId('usage-meter-percentage').element().textContent?.trim()).toBe('85%');
		const percentageEl = screen.getByTestId('usage-meter-percentage');
		expect(percentageEl.element().classList.contains('text-warning')).toBe(true);

		const fill = screen.getByTestId('usage-meter-fill');
		expect(fill.element().classList.contains('bg-warning')).toBe(true);
	});

	it('renders limit reached state (>= 100%) with bg-destructive and text-destructive', async () => {
		const screen = render(UsageMeter, {
			label: 'Projects',
			used: 12,
			max: 10
		});

		// Percentage is capped at 100%
		expect(screen.getByTestId('usage-meter-percentage').element().textContent?.trim()).toBe('100%');
		const percentageEl = screen.getByTestId('usage-meter-percentage');
		expect(percentageEl.element().classList.contains('text-destructive')).toBe(true);

		const fill = screen.getByTestId('usage-meter-fill');
		expect(fill.element().classList.contains('bg-destructive')).toBe(true);
		expect(fill.element().style.width).toBe('100%');
	});

	it('renders unlimited state (max = -1) with unlimited label and subdued track', async () => {
		setLocale('id');
		const screen = render(UsageMeter, {
			label: 'Export',
			used: 42,
			max: -1,
			unit: 'kali'
		});

		expect(screen.getByTestId('usage-meter-count').element().textContent).toContain(
			'42 kali / Tak terbatas'
		);
		expect(screen.getByTestId('usage-meter-unlimited-bar')).toBeDefined();
		expect(document.querySelector('[role="progressbar"]')).toBeNull();
	});
});
