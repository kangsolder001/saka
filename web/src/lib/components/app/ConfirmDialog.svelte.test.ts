import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ConfirmDialog from './ConfirmDialog.svelte';

describe('ConfirmDialog', () => {
	it('renders dialog when open', async () => {
		const onConfirm = vi.fn();
		const screen = render(ConfirmDialog, {
			open: true,
			title: 'Delete Organization',
			description: 'This action cannot be undone.',
			onConfirm
		});

		expect(screen.getByText('Delete Organization')).toBeDefined();
		expect(screen.getByText('This action cannot be undone.')).toBeDefined();
	});

	it('requires typing confirmText when specified', async () => {
		const onConfirm = vi.fn();
		const screen = render(ConfirmDialog, {
			open: true,
			title: 'Delete Organization',
			description: 'Type delete to proceed',
			confirmText: 'delete',
			onConfirm
		});

		const confirmBtn = screen.getByRole('button', { name: 'Continue' });
		expect(confirmBtn.element().hasAttribute('disabled')).toBe(true);
	});
});
