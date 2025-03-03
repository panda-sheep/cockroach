// Copyright 2021 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package descmetadata

import (
	"context"
	"fmt"

	"github.com/cockroachdb/cockroach/pkg/keys"
	"github.com/cockroachdb/cockroach/pkg/kv"
	"github.com/cockroachdb/cockroach/pkg/settings"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/descpb"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/descs"
	"github.com/cockroachdb/cockroach/pkg/sql/schemachanger/scexec"
	"github.com/cockroachdb/cockroach/pkg/sql/sessiondata"
	"github.com/cockroachdb/cockroach/pkg/sql/sessioninit"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlutil"
)

// metadataUpdater which implements scexec.MetaDataUpdater that is used to update
// comments on different schema objects.
type metadataUpdater struct {
	ctx          context.Context
	txn          *kv.Txn
	ieFactory    sqlutil.InternalExecutorFactory
	sessionData  *sessiondata.SessionData
	descriptors  *descs.Collection
	cacheEnabled bool
}

// NewMetadataUpdater creates a new comment updater, which can be used to
// create / destroy metadata (i.e. comments) associated with different
// schema objects.
func NewMetadataUpdater(
	ctx context.Context,
	ieFactory sqlutil.InternalExecutorFactory,
	descriptors *descs.Collection,
	settings *settings.Values,
	txn *kv.Txn,
	sessionData *sessiondata.SessionData,
) scexec.DescriptorMetadataUpdater {
	return metadataUpdater{
		ctx:          ctx,
		txn:          txn,
		ieFactory:    ieFactory,
		sessionData:  sessionData,
		descriptors:  descriptors,
		cacheEnabled: sessioninit.CacheEnabled.Get(settings),
	}
}

// DeleteDatabaseRoleSettings implement scexec.DescriptorMetaDataUpdater.
func (mu metadataUpdater) DeleteDatabaseRoleSettings(ctx context.Context, dbID descpb.ID) error {
	ie := mu.ieFactory.NewInternalExecutor(mu.sessionData)
	rowsDeleted, err := ie.ExecEx(ctx,
		"delete-db-role-setting",
		mu.txn,
		sessiondata.RootUserSessionDataOverride,
		fmt.Sprintf(
			`DELETE FROM %s WHERE database_id = $1`,
			sessioninit.DatabaseRoleSettingsTableName,
		),
		dbID,
	)
	if err != nil {
		return err
	}
	// If the cache is off or if no rows changed, there's no need to bump the
	// table version.
	if !mu.cacheEnabled || rowsDeleted == 0 {
		return nil
	}
	// Bump the table version for the role settings table when we modify it.
	desc, err := mu.descriptors.MutableByID(mu.txn).Table(ctx, keys.DatabaseRoleSettingsTableID)
	if err != nil {
		return err
	}
	desc.MaybeIncrementVersion()
	return mu.descriptors.WriteDesc(ctx, false /*kvTrace*/, desc, mu.txn)
}

// DeleteSchedule implement scexec.DescriptorMetadataUpdater.
func (mu metadataUpdater) DeleteSchedule(ctx context.Context, scheduleID int64) error {
	ie := mu.ieFactory.NewInternalExecutor(mu.sessionData)
	_, err := ie.ExecEx(
		ctx,
		"delete-schedule",
		mu.txn,
		sessiondata.RootUserSessionDataOverride,
		"DELETE FROM system.scheduled_jobs WHERE schedule_id = $1",
		scheduleID,
	)
	return err
}
