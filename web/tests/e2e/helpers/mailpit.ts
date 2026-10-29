const MAILPIT_URL = process.env.MAILPIT_URL || 'http://localhost:8025';

export interface MailpitMessageSummary {
	ID: string;
	Subject: string;
	To: Array<{ Address: string; Name: string }>;
	From: { Address: string; Name: string };
	Created: string;
}

export interface MailpitMessageDetail {
	ID: string;
	Subject: string;
	To: Array<{ Address: string; Name: string }>;
	From: { Address: string; Name: string };
	Text: string;
	HTML: string;
	// Convenience fields for test ergonomics
	subject: string;
	body: string;
	links: string[];
}

export interface GetEmailOptions {
	subject?: RegExp | string;
	pathPattern?: RegExp | string;
	timeoutMs?: number;
}

const EXPECTED_PATTERNS = [
	/\/verify-email/,
	/\/reset-password/,
	/\/confirm-email-change/,
	/\/accept-invitation/
];

/**
 * Extracts links from email content, prioritizing verification, reset, confirmation, and invitation links.
 */
export function extractLinks(html: string, text: string): string[] {
	const content = `${html}\n${text}`;
	const found = new Set<string>();

	// 1. Match HTML href attributes
	const hrefRegex = /href=["'](https?:\/\/[^"']+)["']/gi;
	let match: RegExpExecArray | null;
	while ((match = hrefRegex.exec(content)) !== null) {
		if (match[1]) {
			const cleaned = match[1].replace(/&amp;/g, '&').replace(/[",;)>]+$/, '');
			found.add(cleaned);
		}
	}

	// 2. Match raw URLs in text or html
	const rawUrlRegex = /https?:\/\/[^\s"'<>]+/gi;
	while ((match = rawUrlRegex.exec(content)) !== null) {
		if (match[0]) {
			const cleaned = match[0].replace(/&amp;/g, '&').replace(/[",;)>]+$/, '');
			found.add(cleaned);
		}
	}

	const links = Array.from(found);

	// Sort expected action links first
	links.sort((a, b) => {
		const aExpected = EXPECTED_PATTERNS.some((pat) => pat.test(a));
		const bExpected = EXPECTED_PATTERNS.some((pat) => pat.test(b));
		if (aExpected && !bExpected) return -1;
		if (!aExpected && bExpected) return 1;
		return 0;
	});

	return links;
}

/**
 * Searches for messages sent to a recipient, with polling until a matching message is found or timeout occurs.
 * If subject or pathPattern is specified, it waits until a message matching that pattern arrives.
 */
export async function getLatestEmail(
	toEmail: string,
	optionsOrTimeout: GetEmailOptions | number = 15000
): Promise<MailpitMessageDetail> {
	const options: GetEmailOptions =
		typeof optionsOrTimeout === 'number' ? { timeoutMs: optionsOrTimeout } : optionsOrTimeout;

	const timeoutMs = options.timeoutMs ?? 15000;
	const startTime = Date.now();
	const query = `to:${toEmail}`;
	const searchUrl = `${MAILPIT_URL}/api/v1/search?query=${encodeURIComponent(query)}`;

	while (Date.now() - startTime < timeoutMs) {
		try {
			const res = await fetch(searchUrl);
			if (res.ok) {
				const data = (await res.json()) as { messages?: MailpitMessageSummary[] };
				if (data.messages && data.messages.length > 0) {
					// Sort descending by Created timestamp
					const sorted = [...data.messages].sort(
						(a, b) => new Date(b.Created).getTime() - new Date(a.Created).getTime()
					);

					// Find message matching subject filter if provided
					const candidates = options.subject
						? sorted.filter((m) =>
								typeof options.subject === 'string'
									? m.Subject.toLowerCase().includes(options.subject.toLowerCase())
									: (options.subject as RegExp).test(m.Subject)
							)
						: sorted;

					for (const candidate of candidates) {
						const msgRes = await fetch(`${MAILPIT_URL}/api/v1/message/${candidate.ID}`);
						if (msgRes.ok) {
							const detail = (await msgRes.json()) as {
								ID: string;
								Subject: string;
								To: Array<{ Address: string; Name: string }>;
								From: { Address: string; Name: string };
								Text: string;
								HTML: string;
							};
							const html = detail.HTML || '';
							const text = detail.Text || '';
							const body = html || text;
							const links = extractLinks(html, text);

							// If pathPattern was provided, ensure it matches at least one link or content
							if (options.pathPattern) {
								const hasMatchingLink = links.some((l) =>
									typeof options.pathPattern === 'string'
										? l.includes(options.pathPattern)
										: (options.pathPattern as RegExp).test(l)
								);
								if (!hasMatchingLink) {
									continue;
								}
							}

							return {
								...detail,
								subject: detail.Subject || candidate.Subject || '',
								body,
								links
							};
						}
					}
				}
			}
		} catch {
			// ignore network blips during polling
		}
		await new Promise((resolve) => setTimeout(resolve, 500));
	}

	throw new Error(`Timeout waiting for email sent to ${toEmail} after ${timeoutMs}ms`);
}

/**
 * Extracts a link from an email's HTML or plain text matching a regex or path pattern.
 */
export function extractLinkFromEmail(
	email: MailpitMessageDetail,
	pathPattern?: RegExp | string
): string {
	if (email.links && email.links.length > 0) {
		if (!pathPattern) return email.links[0]!;
		const found = email.links.find((l) =>
			typeof pathPattern === 'string' ? l.includes(pathPattern) : pathPattern.test(l)
		);
		if (found) return found;
	}

	const content = email.HTML || email.Text || '';
	const urlRegex = /https?:\/\/[^\s"'>]+/g;
	const matches = content.match(urlRegex) || [];

	for (const url of matches) {
		const cleaned = url.replace(/&amp;/g, '&').replace(/[",;>]+$/, '');
		if (!pathPattern) return cleaned;
		if (typeof pathPattern === 'string' && cleaned.includes(pathPattern)) return cleaned;
		if (pathPattern instanceof RegExp && pathPattern.test(cleaned)) return cleaned;
	}

	throw new Error(`No matching link found in email with pattern: ${pathPattern}`);
}

/**
 * Convenience helper to wait for the latest email to an address and extract the link.
 */
export async function getLatestEmailLink(
	toEmail: string,
	pathPattern?: RegExp | string,
	timeoutMs = 15000
): Promise<string> {
	const email = await getLatestEmail(toEmail, { pathPattern, timeoutMs });
	return extractLinkFromEmail(email, pathPattern);
}

/**
 * Clear all messages in Mailpit (useful before/after tests).
 */
export async function clearMailpit(): Promise<void> {
	try {
		await fetch(`${MAILPIT_URL}/api/v1/messages`, { method: 'DELETE' });
	} catch {
		// ignore
	}
}
