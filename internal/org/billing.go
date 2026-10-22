package org

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

// BillingContact is whom billing writes to about an organization: its owner.
type BillingContact struct {
	OrganizationName string
	OwnerName        string
	OwnerEmail       string
}

// BillingContact returns the name of the organization and the name and email
// of its owner, for the invoice and payment emails of the billing module (it
// implements billing.OrganizationContacts). Deleted organizations are found
// too, since a late payment for one still has to be reported. NOT_FOUND when
// the organization does not exist or its owner's account is gone. q may be a
// tenant transaction of that organization or a system transaction.
func (s *Service) BillingContact(ctx context.Context, q db.DBTX, orgID uuid.UUID) (BillingContact, error) {
	row, err := orgdbQueries(q).GetOrganizationAnyState(ctx, orgID)
	if isNoRows(err) {
		return BillingContact{}, apperr.NotFound()
	}
	if err != nil {
		return BillingContact{}, fmt.Errorf("org: get organization for billing: %w", err)
	}
	owners, err := s.Users.GetSummaries(ctx, q, []uuid.UUID{row.OwnerID})
	if err != nil {
		return BillingContact{}, fmt.Errorf("org: get owner for billing: %w", err)
	}
	owner, ok := owners[row.OwnerID]
	if !ok || owner.Status == user.StatusDeleted {
		return BillingContact{}, apperr.NotFound()
	}
	return BillingContact{OrganizationName: row.Name, OwnerName: owner.Name, OwnerEmail: owner.Email}, nil
}
