package org

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/db"
)

func orgdbQueries(q db.DBTX) *orgdb.Queries { return orgdb.New(q) }

func getMembershipParams(orgID, userID uuid.UUID) orgdb.GetMembershipParams {
	return orgdb.GetMembershipParams{OrganizationID: orgID, UserID: userID}
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
