// Package awssdk adapts AWS SDK for Go v2 SQS calls into scanner-owned queue
// metadata.
//
// The adapter only calls ListQueues, GetQueueAttributes with an explicit safe
// metadata allowlist, and ListQueueTags. It must not call ReceiveMessage and
// must not request or persist the queue Policy attribute.
package awssdk
