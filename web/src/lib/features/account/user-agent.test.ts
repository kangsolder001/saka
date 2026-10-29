import { describe, expect, it } from 'vitest';
import { describeUserAgent } from './user-agent';

describe('describeUserAgent', () => {
	const browsers = [
		{ name: 'Chrome', token: 'Chrome/120.0.0.0' },
		{ name: 'Firefox', token: 'Firefox/121.0' },
		{ name: 'Safari', token: 'Version/17.0 Safari/605.1.15' },
		{ name: 'Edge', token: 'Edg/120.0.0.0' }
	];

	const oss = [
		{ name: 'Windows', token: 'Windows NT 10.0; Win64; x64' },
		{ name: 'macOS', token: 'Macintosh; Intel Mac OS X 10_15_7' },
		{ name: 'Linux', token: 'X11; Linux x86_64' },
		{ name: 'Android', token: 'Linux; Android 14; Pixel 8' },
		{ name: 'iOS', token: 'iPhone; CPU iPhone OS 17_0 like Mac OS X' }
	];

	// Test full matrix (4 browsers × 5 OS = 20 combinations)
	for (const b of browsers) {
		for (const o of oss) {
			it(`correctly identifies ${b.name} on ${o.name}`, () => {
				// Construct representative User-Agent
				let ua = `Mozilla/5.0 (${o.token}) AppleWebKit/537.36 (KHTML, like Gecko)`;
				if (b.name === 'Edge') {
					ua += ` Chrome/120.0.0.0 Safari/537.36 ${b.token}`;
				} else if (b.name === 'Chrome') {
					ua += ` ${b.token} Safari/537.36`;
				} else if (b.name === 'Safari') {
					ua += ` ${b.token}`;
				} else if (b.name === 'Firefox') {
					ua += ` ${b.token}`;
				}

				const result = describeUserAgent(ua);
				expect(result.browser).toBe(b.name);
				expect(result.os).toBe(o.name);
				expect(result.formatted).toBe(`${b.name} on ${o.name}`);
			});
		}
	}

	it('handles unknown browser and unknown OS fallback', () => {
		const result = describeUserAgent('CustomBot/1.0');
		expect(result.browser).toBe('Unknown Browser');
		expect(result.os).toBe('Unknown OS');
		expect(result.formatted).toBe('Unknown Device');
	});

	it('handles empty or invalid inputs', () => {
		expect(describeUserAgent('').formatted).toBe('Unknown Device');
		expect(describeUserAgent(null as unknown as string).formatted).toBe('Unknown Device');
	});
});
