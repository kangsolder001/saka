package billing

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// UsageReporter reports an organization's usage against its plan
// (*entitlement.Service implements it); billing declares it so it does not
// depend on how the counters are wired.
type UsageReporter interface {
	Usage(ctx context.Context, q db.DBTX, orgID uuid.UUID) (map[string]entitlement.LimitUsage, error)
}

// Transactor runs a function in a transaction pinned to the tenant on ctx;
// *db.DB implements it.
type Transactor interface {
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

var _ Transactor = (*db.DB)(nil)

// HandlerDependencies are the collaborators of Handler.
type HandlerDependencies struct {
	Service *Service
	Usage   UsageReporter
	Tx      Transactor
	// Pool reads the public catalogue, which is not tenant data.
	Pool db.DBTX
}

// Handler exposes the plan and subscription endpoints (spec §8.3).
type Handler struct {
	HandlerDependencies
}

// NewHandler creates the billing handler.
func NewHandler(deps HandlerDependencies) *Handler {
	return &Handler{HandlerDependencies: deps}
}

// Routes registers the endpoints: GET /plans and the webhooks under rg
// (expected to be /api/v1, public: a webhook's signature is its credential)
// and the subscription and invoice endpoints under tenant (expected to be
// org.TenantRoutes, so the caller is an authenticated member of the
// organization). requireVerifiedEmail guards the endpoints that make the
// service email the customer or create a payment (checkout, change-plan).
func (h *Handler) Routes(rg, tenant *gin.RouterGroup, requireVerifiedEmail gin.HandlerFunc) {
	rg.GET("/plans", h.listPlans)
	rg.POST("/webhooks/:provider", h.webhook)
	tenant.GET("/subscription", h.getSubscription)
	h.paymentRoutes(tenant, requireVerifiedEmail)
}

type planResponse struct {
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	PriceAmount int64          `json:"price_amount"`
	Currency    string         `json:"currency"`
	Interval    string         `json:"interval"`
	TrialDays   int            `json:"trial_days"`
	Features    []string       `json:"features"`
	Limits      map[string]int `json:"limits"`
}

// listPlans answers the public catalogue: active plans in display order. Limits
// of -1 mean unlimited.
func (h *Handler) listPlans(c *gin.Context) {
	plans, err := h.Service.ActivePlans(c.Request.Context(), h.Pool)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	items := make([]planResponse, len(plans))
	for i, p := range plans {
		items[i] = planResponse{
			Code: p.Code, Name: p.Name, PriceAmount: p.PriceAmount, Currency: p.Currency,
			Interval: p.Interval, TrialDays: p.TrialDays, Features: p.Features, Limits: p.Limits,
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type limitUsageResponse struct {
	Used int `json:"used"`
	Max  int `json:"max"`
}

type subscriptionResponse struct {
	// Plan is the plan of the subscription itself; EffectivePlan is the one the
	// organization has access to right now (Free once access has lapsed).
	Plan               string                        `json:"plan"`
	Status             Status                        `json:"status"`
	TrialEndsAt        *time.Time                    `json:"trial_ends_at"`
	CurrentPeriodStart time.Time                     `json:"current_period_start"`
	CurrentPeriodEnd   *time.Time                    `json:"current_period_end"`
	CancelAtPeriodEnd  bool                          `json:"cancel_at_period_end"`
	PendingPlan        *string                       `json:"pending_plan"`
	GraceUntil         *time.Time                    `json:"grace_until"`
	EffectivePlan      string                        `json:"effective_plan"`
	Usage              map[string]limitUsageResponse `json:"usage"`
}

// getSubscription answers the current organization's subscription with its
// usage against the limits it has access to. Any member may read it.
func (h *Handler) getSubscription(c *gin.Context) {
	out, err := h.subscriptionView(c.Request.Context())
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscription": out})
}

// subscriptionView reads the current organization's subscription in a
// transaction of its own, pinned to the tenant on ctx.
func (h *Handler) subscriptionView(ctx context.Context) (subscriptionResponse, error) {
	orgID, ok := reqctx.OrgID(ctx)
	if !ok {
		// Wiring bug: the route was mounted without the tenant middleware.
		return subscriptionResponse{}, apperr.Unauthenticated()
	}
	var out subscriptionResponse
	err := h.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		sub, err := h.Service.GetSubscription(ctx, tx, orgID)
		if err != nil {
			return err
		}
		effective, err := h.Service.EffectivePlan(ctx, tx, orgID)
		if err != nil {
			return err
		}
		usage, err := h.Usage.Usage(ctx, tx, orgID)
		if err != nil {
			return err
		}
		out = subscriptionResponse{
			Plan: sub.PlanCode, Status: sub.Status, TrialEndsAt: utc(sub.TrialEndsAt),
			CurrentPeriodStart: sub.CurrentPeriodStart.UTC(), CurrentPeriodEnd: utc(sub.CurrentPeriodEnd),
			CancelAtPeriodEnd: sub.CancelAtPeriodEnd, PendingPlan: sub.PendingPlanCode, GraceUntil: utc(sub.GraceUntil),
			EffectivePlan: effective.Code, Usage: make(map[string]limitUsageResponse, len(usage)),
		}
		for name, u := range usage {
			out.Usage[name] = limitUsageResponse{Used: u.Used, Max: u.Max}
		}
		return nil
	})
	return out, err
}

// utc renders a timestamp in UTC, as every time in the API is (the driver
// returns the server's local zone).
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
