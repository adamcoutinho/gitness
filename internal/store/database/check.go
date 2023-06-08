// Copyright 2022 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform Free Trial License
// that can be found in the LICENSE.md file for this repository.

package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/harness/gitness/internal/store"
	"github.com/harness/gitness/internal/store/database/dbtx"
	"github.com/harness/gitness/types"
	"github.com/harness/gitness/types/enum"

	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

var _ store.CheckStore = (*CheckStore)(nil)

// NewCheckStore returns a new CheckStore.
func NewCheckStore(
	db *sqlx.DB,
	pCache store.PrincipalInfoCache,
) *CheckStore {
	return &CheckStore{
		db:     db,
		pCache: pCache,
	}
}

// CheckStore implements store.CheckStore backed by a relational database.
type CheckStore struct {
	db     *sqlx.DB
	pCache store.PrincipalInfoCache
}

const (
	checkColumns = `
		 check_id
		,check_created_by
		,check_created
		,check_updated
		,check_repo_id
		,check_commit_sha
		,check_uid
		,check_status
		,check_summary
		,check_link
		,check_payload
		,check_metadata
		,check_payload_kind
		,check_payload_version`
)

type check struct {
	ID             int64                 `db:"check_id"`
	CreatedBy      int64                 `db:"check_created_by"`
	Created        int64                 `db:"check_created"`
	Updated        int64                 `db:"check_updated"`
	RepoID         int64                 `db:"check_repo_id"`
	CommitSHA      string                `db:"check_commit_sha"`
	UID            string                `db:"check_uid"`
	Status         enum.CheckStatus      `db:"check_status"`
	Summary        string                `db:"check_summary"`
	Link           string                `db:"check_link"`
	Payload        json.RawMessage       `db:"check_payload"`
	Metadata       json.RawMessage       `db:"check_metadata"`
	PayloadKind    enum.CheckPayloadKind `db:"check_payload_kind"`
	PayloadVersion string                `db:"check_payload_version"`
}

// Upsert creates new or updates an existing status check result.
func (s *CheckStore) Upsert(ctx context.Context, check *types.Check) error {
	const sqlQuery = `
	INSERT INTO checks (
		 check_created_by
		,check_created
		,check_updated
		,check_repo_id
		,check_commit_sha
		,check_uid
		,check_status
		,check_summary
		,check_link
		,check_payload
		,check_metadata
		,check_payload_kind
		,check_payload_version
	) VALUES (
		 :check_created_by
		,:check_created
		,:check_updated
		,:check_repo_id
		,:check_commit_sha
		,:check_uid
		,:check_status
		,:check_summary
		,:check_link
		,:check_payload
		,:check_metadata
		,:check_payload_kind
		,:check_payload_version
	)
	ON CONFLICT (check_repo_id, check_commit_sha, check_uid) DO
	UPDATE SET
		 check_updated = :check_updated
		,check_status = :check_status
		,check_summary = :check_summary
		,check_link = :check_link
		,check_payload = :check_payload
		,check_metadata = :check_metadata
		,check_payload_kind = :check_payload_kind
		,check_payload_version = :check_payload_version
	RETURNING check_id, check_created_by, check_created`

	db := dbtx.GetAccessor(ctx, s.db)

	query, arg, err := db.BindNamed(sqlQuery, mapInternalCheck(check))
	if err != nil {
		return processSQLErrorf(err, "Failed to bind status check object")
	}

	if err = db.QueryRowContext(ctx, query, arg...).Scan(&check.ID, &check.CreatedBy, &check.Created); err != nil {
		return processSQLErrorf(err, "Upsert query failed")
	}

	return nil
}

// List returns a list of status check results for a specific commit in a repo.
func (s *CheckStore) List(ctx context.Context, repoID int64, commitSHA string) ([]*types.Check, error) {
	stmt := builder.
		Select(checkColumns).
		From("checks").
		Where("check_repo_id = ?", repoID).
		Where("check_commit_sha = ?", commitSHA).
		OrderBy("check_updated desc")

	sql, args, err := stmt.ToSql()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to convert query to sql")
	}

	dst := make([]*check, 0)

	db := dbtx.GetAccessor(ctx, s.db)

	if err = db.SelectContext(ctx, &dst, sql, args...); err != nil {
		return nil, processSQLErrorf(err, "Failed to execute list status checks query")
	}

	result, err := s.mapSliceCheck(ctx, dst)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListRecent returns a list of recently executed status checks in a repository.
func (s *CheckStore) ListRecent(ctx context.Context, repoID int64, since time.Time) ([]string, error) {
	stmt := builder.
		Select("distinct check_uid").
		From("checks").
		Where("check_repo_id = ?", repoID).
		Where("check_created > ?", since.UnixMilli()).
		OrderBy("check_uid")

	sql, args, err := stmt.ToSql()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to convert query to sql")
	}

	dst := make([]string, 0)

	db := dbtx.GetAccessor(ctx, s.db)

	if err = db.SelectContext(ctx, &dst, sql, args...); err != nil {
		return nil, processSQLErrorf(err, "Failed to execute list recent status checks query")
	}

	return dst, nil
}

func mapInternalCheck(c *types.Check) *check {
	m := &check{
		ID:             c.ID,
		CreatedBy:      c.CreatedBy,
		Created:        c.Created,
		Updated:        c.Updated,
		RepoID:         c.RepoID,
		CommitSHA:      c.CommitSHA,
		UID:            c.UID,
		Status:         c.Status,
		Summary:        c.Summary,
		Link:           c.Link,
		Payload:        c.Payload.Data,
		Metadata:       c.Metadata,
		PayloadKind:    c.Payload.Kind,
		PayloadVersion: c.Payload.Version,
	}

	return m
}

func mapCheck(c *check) *types.Check {
	return &types.Check{
		ID:        c.ID,
		CreatedBy: c.CreatedBy,
		Created:   c.Created,
		Updated:   c.Updated,
		RepoID:    c.RepoID,
		CommitSHA: c.CommitSHA,
		UID:       c.UID,
		Status:    c.Status,
		Summary:   c.Summary,
		Link:      c.Link,
		Metadata:  c.Metadata,
		Payload: types.CheckPayload{
			Version: c.PayloadVersion,
			Kind:    c.PayloadKind,
			Data:    c.Payload,
		},
		ReportedBy: types.PrincipalInfo{},
	}
}

func (s *CheckStore) mapSliceCheck(ctx context.Context, checks []*check) ([]*types.Check, error) {
	// collect all principal IDs
	ids := make([]int64, len(checks))
	for i, req := range checks {
		ids[i] = req.CreatedBy
	}

	// pull principal infos from cache
	infoMap, err := s.pCache.Map(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to load status check principal reporters: %w", err)
	}

	// attach the principal infos back to the slice items
	m := make([]*types.Check, len(checks))
	for i, c := range checks {
		m[i] = mapCheck(c)
		if reportedBy, ok := infoMap[c.CreatedBy]; ok {
			m[i].ReportedBy = *reportedBy
		}
	}

	return m, nil
}
