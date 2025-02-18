//go:build integration

package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/context-labs/mongodb-nats-connector/test/harness"
)

func TestMongoRenameCollection(t *testing.T) {
	ctx := context.Background()
	h := harness.New(t, harness.FromEnv())

	h.MustStartContainer(ctx, harness.Connector)
	t.Cleanup(func() {
		h.MustStopContainer(ctx, harness.Connector)
		assert.NoError(t, h.MongoClient.Database("test-connector").Drop(ctx))
		assert.NoError(t, h.MongoClient.Database("resume-tokens").Drop(ctx))
		assert.NoError(t, h.NatsJs.PurgeStream("COLL1"))
		assert.NoError(t, h.NatsJs.PurgeStream("COLL2"))
	})

	h.MustWaitForConnector(10 * time.Second)

	h.MustMongoRenameCollection(ctx, "test-connector", "coll1", "coll3")

	t.Run("does not publish rename message", func(t *testing.T) {
		h.MustNotReceiveNatsMsg("COLL1.rename", 1*time.Second)
	})

	t.Run("does not publish invalidate message", func(t *testing.T) {
		h.MustNotReceiveNatsMsg("COLL1.invalidate", 1*time.Second)
	})

	t.Run("does not crash connector", func(t *testing.T) {
		h.MustEnsureConnectorIsUpFor(1 * time.Second)
	})
}
