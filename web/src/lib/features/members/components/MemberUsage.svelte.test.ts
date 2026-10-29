import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import MemberUsage from './MemberUsage.svelte';

describe('MemberUsage', () => {
	it('renders limited usage when max is positive number', async () => {
		const screen = render(MemberUsage, {
			used: 3,
			max: 10
		});

		const el = screen.getByTestId('member-usage');
		expect(el.element().textContent).toContain('3 / 10');
	});

	it('renders unlimited usage when max is -1', async () => {
		const screen = render(MemberUsage, {
			used: 5,
			max: -1
		});

		const el = screen.getByTestId('member-usage');
		expect(el.element().textContent).toContain('5');
		expect(el.element().textContent).not.toContain('/');
	});

	it('renders unlimited usage when max is undefined', async () => {
		const screen = render(MemberUsage, {
			used: 7
		});

		const el = screen.getByTestId('member-usage');
		expect(el.element().textContent).toContain('7');
		expect(el.element().textContent).not.toContain('/');
	});
});
