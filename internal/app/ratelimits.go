package app

import (
	"net/http"
	"time"

	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
)

// Rate limits applied at the edge of the API, on top of the per-account rules
// inside the auth module (spec §5.4, §11.3). They exist to keep cheap or
// expensive anonymous endpoints from being hammered; the numbers are
// deliberately generous so a real user, an office behind one NAT address or a
// payment gateway retrying a notification never notices them.
var (
	// RuleWebhook counts payment notifications per IP. A gateway sends a few
	// notifications per payment and retries on failures, from a handful of
	// addresses; 600 a minute per address is far above that and far below a flood.
	RuleWebhook = ratelimit.Rule{Name: "webhook", Limit: 600, Window: time.Minute}
	// RuleInvitationLink counts lookups, acceptances and declines of an
	// invitation by token, per IP. Tokens carry 256 bits, so guessing is not the
	// threat; it is the unauthenticated database lookup.
	RuleInvitationLink = ratelimit.Rule{Name: "invitation_link", Limit: 60, Window: time.Minute}
	// RuleAuthToken counts the anonymous endpoints that exchange a token
	// (verify-email, reset-password, refresh, confirm email change) per IP.
	RuleAuthToken = ratelimit.Rule{Name: "auth_token", Limit: 60, Window: time.Minute}
	// RuleLoginAttempts counts every login request per IP, successful or not.
	// The per-account rule only counts failures of one email, so without this an
	// attacker could make the server hash passwords for endless distinct emails
	// (Argon2id is deliberately expensive).
	RuleLoginAttempts = ratelimit.Rule{Name: "login_attempts", Limit: 60, Window: time.Minute}
	// RulePublicCatalog counts GET /plans per IP: it is public and reads the
	// database on every call.
	RulePublicCatalog = ratelimit.Rule{Name: "public_catalog", Limit: 120, Window: time.Minute}
	// RuleCreateOrganization counts POST /orgs per user: every call writes an
	// organization, a membership and a subscription.
	RuleCreateOrganization = ratelimit.Rule{Name: "create_organization", Limit: 10, Window: time.Hour}
)

// publicRouteLimits are the per-IP limits keyed by route template.
func publicRouteLimits() []httpx.RouteLimit {
	const v1 = "/api/v1"
	return []httpx.RouteLimit{
		// The signature is the credential of a webhook, so an unreachable Redis
		// must not make us drop a genuine payment notification.
		{Method: http.MethodPost, Path: v1 + "/webhooks/:provider", Rule: RuleWebhook, FailOpen: true},
		{Method: http.MethodGet, Path: v1 + "/invitations/:token", Rule: RuleInvitationLink},
		{Method: http.MethodPost, Path: v1 + "/invitations/:token/accept", Rule: RuleInvitationLink},
		{Method: http.MethodPost, Path: v1 + "/invitations/:token/decline", Rule: RuleInvitationLink},
		{Method: http.MethodPost, Path: v1 + "/auth/verify-email", Rule: RuleAuthToken},
		{Method: http.MethodPost, Path: v1 + "/auth/reset-password", Rule: RuleAuthToken},
		{Method: http.MethodPost, Path: v1 + "/auth/refresh", Rule: RuleAuthToken},
		{Method: http.MethodPost, Path: v1 + "/me/email/confirm", Rule: RuleAuthToken},
		{Method: http.MethodPost, Path: v1 + "/auth/login", Rule: RuleLoginAttempts},
		{Method: http.MethodGet, Path: v1 + "/plans", Rule: RulePublicCatalog},
	}
}
