package billing

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/pagination"
)

// paymentRoutes mounts the endpoints that deal with money on the tenant group.
// Only owners manage billing (spec §7.2): the role is checked here for a quick
// 403 and again in the service, which is what protects non-HTTP callers.
func (h *Handler) paymentRoutes(tenant *gin.RouterGroup, requireVerifiedEmail gin.HandlerFunc) {
	owner := org.Require(org.ActionManageBilling)
	tenant.POST("/subscription/checkout", owner, requireVerifiedEmail, h.checkout)
	tenant.POST("/subscription/change-plan", owner, requireVerifiedEmail, h.changePlan)
	tenant.POST("/subscription/cancel", owner, h.cancel)
	tenant.POST("/subscription/resume", owner, h.resume)
	tenant.GET("/invoices", owner, h.listInvoices)
	tenant.GET("/invoices/:id", owner, h.getInvoice)
}

type invoiceResponse struct {
	ID          uuid.UUID     `json:"id"`
	Number      string        `json:"number"`
	Kind        InvoiceKind   `json:"kind"`
	Plan        string        `json:"plan"`
	PlanName    string        `json:"plan_name"`
	Amount      int64         `json:"amount"`
	Currency    string        `json:"currency"`
	Status      InvoiceStatus `json:"status"`
	PeriodStart time.Time     `json:"period_start"`
	PeriodEnd   time.Time     `json:"period_end"`
	DueAt       time.Time     `json:"due_at"`
	PaidAt      *time.Time    `json:"paid_at"`
	// PaymentURL is where to pay; null once the invoice can no longer be paid.
	PaymentURL *string   `json:"payment_url"`
	CreatedAt  time.Time `json:"created_at"`
}

func invoiceJSON(i Invoice) invoiceResponse {
	return invoiceResponse{
		ID: i.ID, Number: i.Number, Kind: i.Kind, Plan: i.PlanCode, PlanName: i.PlanName, Amount: i.Amount,
		Currency: i.Currency, Status: i.Status, PeriodStart: i.PeriodStart.UTC(), PeriodEnd: i.PeriodEnd.UTC(),
		DueAt: i.DueAt.UTC(), PaidAt: utc(i.PaidAt), PaymentURL: i.PaymentURL, CreatedAt: i.CreatedAt.UTC(),
	}
}

type checkoutRequest struct {
	Plan string `json:"plan" validate:"required,max=64"`
}

// checkout answers 201 with the new invoice, or 200 with the invoice that was
// already waiting for payment for that plan. trial_started tells the client
// the organization is on a trial of the plan from now on.
func (h *Handler) checkout(c *gin.Context) {
	var req checkoutRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	res, err := h.Service.Checkout(ctx, req.Plan)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	sub, err := h.subscriptionView(ctx)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	status := http.StatusCreated
	if res.Reused {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"invoice": invoiceJSON(res.Invoice), "subscription": sub, "trial_started": res.TrialStarted})
}

type limitWarningResponse struct {
	Limit string `json:"limit"`
	Used  int    `json:"used"`
	Max   int    `json:"max"`
}

// changePlan answers what the change did: an invoice to pay (upgrade), a
// downgrade booked for the end of the period (with warnings about limits the
// smaller plan would exceed), or a scheduled downgrade taken back.
func (h *Handler) changePlan(c *gin.Context) {
	var req checkoutRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	res, err := h.Service.ChangePlan(ctx, req.Plan)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	sub, err := h.subscriptionView(ctx)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	warnings := make([]limitWarningResponse, len(res.Warnings))
	for i, w := range res.Warnings {
		warnings[i] = limitWarningResponse(w)
	}
	status := http.StatusOK
	body := gin.H{"subscription": sub, "invoice": nil, "scheduled": res.Scheduled, "cleared": res.Cleared, "trial_started": false, "warnings": warnings}
	if res.Checkout != nil {
		body["invoice"] = invoiceJSON(res.Checkout.Invoice)
		body["trial_started"] = res.Checkout.TrialStarted
		if !res.Checkout.Reused {
			status = http.StatusCreated
		}
	}
	c.JSON(status, body)
}

func (h *Handler) cancel(c *gin.Context) {
	h.setCancellation(c, h.Service.CancelSubscription)
}

func (h *Handler) resume(c *gin.Context) {
	h.setCancellation(c, h.Service.ResumeSubscription)
}

func (h *Handler) setCancellation(c *gin.Context, do func(context.Context) error) {
	ctx := c.Request.Context()
	if err := do(ctx); err != nil {
		httpx.Error(c, err)
		return
	}
	sub, err := h.subscriptionView(ctx)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscription": sub})
}

func (h *Handler) listInvoices(c *gin.Context) {
	params, err := pagination.ParseQuery(c.Request.URL.Query())
	if err != nil {
		httpx.Error(c, err)
		return
	}
	rows, err := h.Service.ListInvoices(c.Request.Context(), params)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	page := pagination.Paginate(rows, params.Limit, Invoice.CursorOf)
	items := make([]invoiceResponse, len(page.Items))
	for i, inv := range page.Items {
		items[i] = invoiceJSON(inv)
	}
	c.JSON(http.StatusOK, pagination.Page[invoiceResponse]{Items: items, NextCursor: page.NextCursor})
}

func (h *Handler) getInvoice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Error(c, apperr.NotFound())
		return
	}
	inv, err := h.Service.GetInvoice(c.Request.Context(), id)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invoice": invoiceJSON(inv)})
}

// maxWebhookBytes caps a notification body; real ones are a few hundred bytes.
const maxWebhookBytes = 1 << 20

// webhook receives a notification from a payment gateway. It is public: the
// signature, checked by the gateway's own code, is the credential. A gateway
// treats any 2xx as "received" and retries everything else, so the status code
// is the protocol: 200 when the event is stored (and, from the next step on,
// processed), 401 for a bad signature, 500 when processing failed and should
// be retried.
func (h *Handler) webhook(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBytes)
	if err := h.Service.HandleWebhook(c.Request.Context(), c.Param("provider"), c.Request); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
