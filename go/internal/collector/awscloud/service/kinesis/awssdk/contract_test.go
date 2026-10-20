// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAPIInterfacesExcludeRecordMediaAndMutationAPIs is the load-bearing
// acceptance proof for issue #750. The three sub-API interfaces
// (dataStreamsAPI, firehoseAPI, videoAPI) are the only way the adapter reaches
// the AWS Kinesis, Firehose, and Kinesis Video SDKs, so asserting their exact
// method shape proves the forbidden record-plane, media-plane, and mutation
// APIs are unreachable from this code path.
//
// Forbidden Data Streams APIs: PutRecord, PutRecords, GetRecords,
// GetShardIterator, MergeShards, SplitShard, CreateStream, DeleteStream,
// UpdateStreamMode, IncreaseStreamRetentionPeriod,
// DecreaseStreamRetentionPeriod, StartStreamEncryption, StopStreamEncryption.
//
// Forbidden Firehose APIs: CreateDeliveryStream, UpdateDestination,
// DeleteDeliveryStream, PutDeliveryStreamEncryptionConfiguration,
// StartDeliveryStreamEncryption, PutRecord, PutRecordBatch.
//
// Forbidden Kinesis Video APIs: GetMedia, PutMedia, GetMediaForFragmentList,
// CreateStream, UpdateStream, DeleteStream, UpdateDataRetention.
func TestAPIInterfacesExcludeRecordMediaAndMutationAPIs(t *testing.T) {
	cases := []struct {
		name      string
		iface     reflect.Type
		want      map[string]bool
		forbidden []string
	}{
		{
			name:  "dataStreamsAPI",
			iface: reflect.TypeOf((*dataStreamsAPI)(nil)).Elem(),
			want: map[string]bool{
				"ListStreams":           true,
				"DescribeStreamSummary": true,
				"ListTagsForStream":     true,
			},
			forbidden: []string{
				"PutRecord", "GetRecords", "GetShardIterator", "MergeShards",
				"SplitShard", "Create", "Delete", "Update", "Increase",
				"Decrease", "StartStreamEncryption", "StopStreamEncryption",
				"DescribeStreamConsumer", "RegisterStreamConsumer",
			},
		},
		{
			name:  "firehoseAPI",
			iface: reflect.TypeOf((*firehoseAPI)(nil)).Elem(),
			want: map[string]bool{
				"ListDeliveryStreams":       true,
				"DescribeDeliveryStream":    true,
				"ListTagsForDeliveryStream": true,
			},
			forbidden: []string{
				"PutRecord", "Create", "Delete", "Update", "Start", "Stop",
				"PutDeliveryStreamEncryptionConfiguration", "TagDeliveryStream",
				"UntagDeliveryStream",
			},
		},
		{
			name:  "videoAPI",
			iface: reflect.TypeOf((*videoAPI)(nil)).Elem(),
			want: map[string]bool{
				"ListStreams":       true,
				"ListTagsForStream": true,
			},
			forbidden: []string{
				"GetMedia", "PutMedia", "GetMediaForFragmentList", "Create",
				"Delete", "Update", "GetDataEndpoint", "GetClip", "GetImages",
				"ListFragments",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			have := map[string]bool{}
			for i := 0; i < tc.iface.NumMethod(); i++ {
				have[tc.iface.Method(i).Name] = true
			}
			for name := range tc.want {
				if !have[name] {
					t.Errorf("%s missing required method %q", tc.name, name)
				}
			}
			for name := range have {
				if !tc.want[name] {
					t.Errorf("%s exposes unexpected method %q; metadata-only contract violated", tc.name, name)
				}
			}
			for name := range have {
				for _, forbidden := range tc.forbidden {
					if strings.Contains(name, forbidden) {
						t.Errorf("%s method %q contains forbidden substring %q", tc.name, name, forbidden)
					}
				}
			}
		})
	}
}
