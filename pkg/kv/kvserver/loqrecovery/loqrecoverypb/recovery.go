// Copyright 2021 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package loqrecoverypb

import (
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/util/keysutil"
	"github.com/cockroachdb/cockroach/pkg/util/log/eventpb"
	"github.com/cockroachdb/cockroach/pkg/util/log/logpb"
	_ "github.com/cockroachdb/cockroach/pkg/util/uuid" // needed for recovery.proto
	"github.com/cockroachdb/errors"
	"github.com/gogo/protobuf/proto"
)

// RecoveryKey is an alias for RKey that is used to make it
// yaml serializable. Caution must be taken to use produced
// representation outside of tests.
type RecoveryKey roachpb.RKey

// MarshalYAML implements Marshaler interface.
func (r RecoveryKey) MarshalYAML() (interface{}, error) {
	return roachpb.RKey(r).String(), nil
}

// UnmarshalYAML implements Unmarshaler interface.
func (r *RecoveryKey) UnmarshalYAML(fn func(interface{}) error) error {
	var pretty string
	if err := fn(&pretty); err != nil {
		return err
	}
	scanner := keysutil.MakePrettyScanner(nil /* tableParser */)
	key, err := scanner.Scan(pretty)
	if err != nil {
		return errors.Wrapf(err, "failed to parse key %s", pretty)
	}
	*r = RecoveryKey(key)
	return nil
}

// AsRKey returns key as a cast to RKey.
func (r RecoveryKey) AsRKey() roachpb.RKey {
	return roachpb.RKey(r)
}

func (m ReplicaUpdate) String() string {
	return proto.CompactTextString(&m)
}

// NodeID is a NodeID on which this replica update should be applied.
func (m ReplicaUpdate) NodeID() roachpb.NodeID {
	return m.NewReplica.NodeID
}

// StoreID is a StoreID on which this replica update should be applied.
func (m ReplicaUpdate) StoreID() roachpb.StoreID {
	return m.NewReplica.StoreID
}

// Replica gets replica for the store where this info and range
// descriptor were collected. Returns err if it can't find replica
// descriptor for the store it originated from.
func (m *ReplicaInfo) Replica() (roachpb.ReplicaDescriptor, error) {
	if d, ok := m.Desc.GetReplicaDescriptor(m.StoreID); ok {
		return d, nil
	}
	return roachpb.ReplicaDescriptor{}, errors.Errorf(
		"invalid replica info: its own store s%d is not present in descriptor replicas %s",
		m.StoreID, m.Desc)
}

// AsStructuredLog creates a structured log entry from the record.
func (m *ReplicaRecoveryRecord) AsStructuredLog() eventpb.DebugRecoverReplica {
	return eventpb.DebugRecoverReplica{
		CommonEventDetails: logpb.CommonEventDetails{
			Timestamp: m.Timestamp,
		},
		CommonDebugEventDetails: eventpb.CommonDebugEventDetails{
			NodeID: int32(m.NewReplica.NodeID),
		},
		RangeID:           int64(m.RangeID),
		StoreID:           int64(m.NewReplica.StoreID),
		SurvivorReplicaID: int32(m.OldReplicaID),
		UpdatedReplicaID:  int32(m.NewReplica.ReplicaID),
		StartKey:          m.StartKey.AsRKey().String(),
		EndKey:            m.EndKey.AsRKey().String(),
	}
}
