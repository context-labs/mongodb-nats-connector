package connector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/context-labs/mongodb-nats-connector/internal/mongo"
	"github.com/context-labs/mongodb-nats-connector/internal/nats"
)

func TestNew(t *testing.T) {
	t.Run("should create connector with defaults", func(t *testing.T) {
		var (
			mongoClient = &mockMongoClient{}
			natsClient  = &mockNatsClient{}
		)

		conn, err := New(
			withMongoClient(mongoClient), // avoid connecting to a real mongo instance
			withNatsClient(natsClient),   // avoid connecting to a real nats instance
		)

		require.NoError(t, err)
		require.Equal(t, slog.LevelInfo, conn.options.logLevel)
		require.Empty(t, conn.options.mongoUri)
		require.Equal(t, mongoClient, conn.options.mongoClient)
		require.Empty(t, conn.options.natsUrl)
		require.Equal(t, natsClient, conn.options.natsClient)
		require.NotNil(t, conn.options.ctx)
		require.NotNil(t, conn.options.stop)
		require.Empty(t, conn.options.serverAddr)
		require.NotNil(t, conn.logger)
		require.NotNil(t, conn.server)
		require.Empty(t, conn.options.collections)
	})
	t.Run("should create connector with all supported log levels", func(t *testing.T) {
		var (
			mongoClient = &mockMongoClient{}
			natsClient  = &mockNatsClient{}
		)

		supportedLevels := map[string]slog.Level{
			"info":  slog.LevelInfo,
			"debug": slog.LevelDebug,
			"warn":  slog.LevelWarn,
			"error": slog.LevelError,
		}

		for levelStr, level := range supportedLevels {
			conn, err := New(
				withMongoClient(mongoClient), // avoid connecting to a real mongo instance
				withNatsClient(natsClient),   // avoid connecting to a real nats instance
				WithLogLevel(levelStr),
			)

			require.NoError(t, err)
			require.Equal(t, level, conn.options.logLevel)
		}
	})
	t.Run("should create connector with given options", func(t *testing.T) {
		var (
			logLevel    = "debug"
			mongoUri    = "localhost:27017"
			mongoClient = &mockMongoClient{}
			natsUrl     = "localhost:4222"
			natsClient  = &mockNatsClient{}
			serverAddr  = ":8080"
		)

		conn, err := New(
			WithLogLevel(logLevel),
			WithMongoUri(mongoUri),
			withMongoClient(mongoClient),
			WithNatsUrl(natsUrl),
			withNatsClient(natsClient),
			WithContext(context.TODO()),
			WithServerAddr(serverAddr),
		)

		require.NoError(t, err)
		require.Equal(t, slog.LevelDebug, conn.options.logLevel)
		require.Equal(t, mongoUri, conn.options.mongoUri)
		require.Equal(t, mongoClient, conn.options.mongoClient)
		require.Equal(t, natsUrl, conn.options.natsUrl)
		require.Equal(t, natsClient, conn.options.natsClient)
		require.NotNil(t, conn.options.ctx)
		require.NotNil(t, conn.options.stop)
		require.Equal(t, serverAddr, conn.options.serverAddr)
		require.NotNil(t, conn.logger)
		require.NotNil(t, conn.server)
		require.Empty(t, conn.options.collections)
	})
	t.Run("should create connector with collection defaults", func(t *testing.T) {
		var (
			mongoClient = &mockMongoClient{}
			natsClient  = &mockNatsClient{}
			dbName      = "connector-db"
			collName    = "coll1"
		)

		conn, err := New(
			withMongoClient(mongoClient), // avoid connecting to a real mongo instance
			withNatsClient(natsClient),   // avoid connecting to a real nats instance
			WithCollection(dbName, collName),
		)

		require.NoError(t, err)
		require.Contains(t, conn.options.collections, &collection{
			dbName:                       dbName,
			collName:                     collName,
			changeStreamPreAndPostImages: false,
			tokensDbName:                 "resume-tokens",
			tokensCollName:               collName,
			tokensCollCapped:             false,
			tokensCollSizeInBytes:        0,
			streamName:                   strings.ToUpper(collName),
		})
	})
	t.Run("should create connector with given collection options", func(t *testing.T) {
		var (
			mongoClient     = &mockMongoClient{}
			natsClient      = &mockNatsClient{}
			dbName          = "connector-db"
			collName        = "coll1"
			tokensDbName    = "tokens-db"
			tokensCollName  = "coll1-tokens"
			collSizeInBytes = int64(2048)
			streamName      = "coll1-stream"
		)

		conn, err := New(
			withMongoClient(mongoClient), // avoid connecting to a real mongo instance
			withNatsClient(natsClient),   // avoid connecting to a real nats instance
			WithCollection(dbName, collName,
				WithChangeStreamPreAndPostImages(),
				WithTokensDbName(tokensDbName),
				WithTokensCollName(tokensCollName),
				WithTokensCollCapped(collSizeInBytes),
				WithStreamName(streamName),
			),
		)

		require.NoError(t, err)
		require.Contains(t, conn.options.collections, &collection{
			dbName:                       dbName,
			collName:                     collName,
			changeStreamPreAndPostImages: true,
			tokensDbName:                 tokensDbName,
			tokensCollName:               tokensCollName,
			tokensCollCapped:             true,
			tokensCollSizeInBytes:        collSizeInBytes,
			streamName:                   streamName,
		})
	})
	t.Run("should return error cause dbName is missing", func(t *testing.T) {
		conn, err := New(
			WithCollection("", "test-coll"),
		)

		require.Nil(t, conn)
		require.EqualError(t, err, ErrDbNameMissing.Error())
	})
	t.Run("should return error cause collName is missing", func(t *testing.T) {
		conn, err := New(
			WithCollection("test-db", ""),
		)

		require.Nil(t, conn)
		require.EqualError(t, err, ErrCollNameMissing.Error())
	})
	t.Run("should return error cause collSizeInBytes is less than 0", func(t *testing.T) {
		conn, err := New(
			WithCollection("test-db", "test-coll", WithTokensCollCapped(-1)),
		)

		require.Nil(t, conn)
		require.EqualError(t, err, ErrInvalidCollSizeInBytes.Error())
	})
	t.Run("should return error cause collSizeInBytes is 0", func(t *testing.T) {
		conn, err := New(
			WithCollection("test-db", "test-coll", WithTokensCollCapped(0)),
		)

		require.Nil(t, conn)
		require.EqualError(t, err, ErrInvalidCollSizeInBytes.Error())
	})
	t.Run("should return error cause tokens cannot be stored in the collection to be watched", func(t *testing.T) {
		var (
			dbName   = "test-db"
			collName = "test-coll"
		)

		conn, err := New(
			WithCollection(dbName, collName, WithTokensDbName(dbName), WithTokensCollName(collName)),
		)

		require.Nil(t, conn)
		require.EqualError(t, err, ErrInvalidDbAndCollNames.Error())
	})
}

func TestConnector_Run(t *testing.T) {
	t.Run("should run connector and ", func(t *testing.T) {
		var (
			mongoClient     = &mockMongoClient{}
			natsClient      = &mockNatsClient{}
			ctx, cancel     = context.WithCancel(context.Background())
			dbName          = "connector-db"
			collName        = "coll1"
			tokensDbName    = "tokens-db"
			tokensCollName  = "coll1-tokens"
			collSizeInBytes = int64(2048)
			streamName      = "coll1-stream"
			subj            = "subj"
			msgId           = "msgId"
			data            = []byte("event")
		)
		defer cancel()

		conn, _ := New(
			withMongoClient(mongoClient), // avoid connecting to a real mongo instance
			withNatsClient(natsClient),   // avoid connecting to a real nats instance
			WithServerAddr(":0"),
			WithContext(ctx),
			WithCollection(dbName, collName,
				WithChangeStreamPreAndPostImages(),
				WithTokensDbName(tokensDbName),
				WithTokensCollName(tokensCollName),
				WithTokensCollCapped(collSizeInBytes),
				WithStreamName(streamName),
			),
		)

		errCh := make(chan error)
		go func() {
			errCh <- conn.Run()
		}()

		t.Run("create watchable collections", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return mongoClient.CollectionWasCreated(mongo.CreateCollectionOptions{
					DbName:                       dbName,
					CollName:                     collName,
					Capped:                       false,
					SizeInBytes:                  0,
					ChangeStreamPreAndPostImages: true,
				})
			}, 1*time.Second, 100*time.Millisecond)
		})

		t.Run("create resume tokens collections", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return mongoClient.CollectionWasCreated(mongo.CreateCollectionOptions{
					DbName:                       tokensDbName,
					CollName:                     tokensCollName,
					Capped:                       true,
					SizeInBytes:                  collSizeInBytes,
					ChangeStreamPreAndPostImages: false,
				})
			}, 1*time.Second, 100*time.Millisecond)
		})

		t.Run("add nats streams", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return natsClient.StreamWasAdded(nats.AddStreamOptions{
					StreamName: streamName,
				})
			}, 1*time.Second, 100*time.Millisecond)
		})

		t.Run("watch collections", func(t *testing.T) {
			require.Eventually(t, func() bool {
				return mongoClient.CollectionWasWatched(mongo.WatchCollectionOptions{
					WatchedDbName:          dbName,
					WatchedCollName:        collName,
					ResumeTokensDbName:     tokensDbName,
					ResumeTokensCollName:   tokensCollName,
					ResumeTokensCollCapped: true,
					StreamName:             streamName,
				})
			}, 1*time.Second, 100*time.Millisecond)
		})

		t.Run("publish change event messages", func(t *testing.T) {
			mongoClient.SimulateChangeEvents(subj, msgId, data)

			require.Eventually(t, func() bool {
				return natsClient.MessageWasPublished(nats.PublishOptions{Subj: subj, MsgId: msgId, Data: data})
			}, 1*time.Second, 100*time.Millisecond)
		})

		t.Run("shut down and close clients when context is cancelled", func(t *testing.T) {
			cancel() // stop the connector by canceling context
			err := <-errCh
			require.NotNil(t, err)
			require.True(t, mongoClient.closed)
			require.True(t, natsClient.closed)
		})
	})
	t.Run("should stop connector and return error if collection creation fails", func(t *testing.T) {
		var (
			createCollErr = errors.New("create collection error")
			mongoClient   = &mockMongoClient{
				createCollectionErr: createCollErr,
			}
			natsClient      = &mockNatsClient{}
			ctx, cancel     = context.WithCancel(context.Background())
			dbName          = "connector-db"
			collName        = "coll1"
			tokensDbName    = "tokens-db"
			tokensCollName  = "coll1-tokens"
			collSizeInBytes = int64(2048)
			streamName      = "coll1-stream"
		)
		defer cancel()

		conn, _ := New(
			withMongoClient(mongoClient), // avoid connecting to a real mongo instance
			withNatsClient(natsClient),   // avoid connecting to a real nats instance
			WithContext(ctx),
			WithCollection(dbName, collName,
				WithChangeStreamPreAndPostImages(),
				WithTokensDbName(tokensDbName),
				WithTokensCollName(tokensCollName),
				WithTokensCollCapped(collSizeInBytes),
				WithStreamName(streamName),
			),
		)

		err := conn.Run()
		require.ErrorIs(t, err, createCollErr)
	})
	t.Run("should stop connector and return error if stream add fails", func(t *testing.T) {
		var (
			addStreamErr = errors.New("add stream error")
			mongoClient  = &mockMongoClient{}
			natsClient   = &mockNatsClient{
				addStreamErr: addStreamErr,
			}
			ctx, cancel     = context.WithCancel(context.Background())
			dbName          = "connector-db"
			collName        = "coll1"
			tokensDbName    = "tokens-db"
			tokensCollName  = "coll1-tokens"
			collSizeInBytes = int64(2048)
			streamName      = "coll1-stream"
		)
		defer cancel()

		conn, _ := New(
			withMongoClient(mongoClient), // avoid connecting to a real mongo instance
			withNatsClient(natsClient),   // avoid connecting to a real nats instance
			WithContext(ctx),
			WithCollection(dbName, collName,
				WithChangeStreamPreAndPostImages(),
				WithTokensDbName(tokensDbName),
				WithTokensCollName(tokensCollName),
				WithTokensCollCapped(collSizeInBytes),
				WithStreamName(streamName),
			),
		)

		err := conn.Run()
		require.ErrorIs(t, err, addStreamErr)
	})
}

type mockMongoClient struct {
	closed     bool
	name       string
	monitorErr error

	muc                  sync.Mutex
	createCollectionOpts []mongo.CreateCollectionOptions
	createCollectionErr  error

	muw                 sync.Mutex
	watchCollectionOpts []mongo.WatchCollectionOptions
	watchCollectionErr  error
}

func (m *mockMongoClient) Close() error {
	m.closed = true
	return nil
}

func (m *mockMongoClient) Name() string {
	return m.name
}

func (m *mockMongoClient) Monitor(_ context.Context) error {
	return m.monitorErr
}

func (m *mockMongoClient) CreateCollection(_ context.Context, opts *mongo.CreateCollectionOptions) error {
	if m.createCollectionErr != nil {
		return m.createCollectionErr
	}
	m.muc.Lock()
	defer m.muc.Unlock()
	m.createCollectionOpts = append(m.createCollectionOpts, *opts)
	return nil
}

func (m *mockMongoClient) CollectionWasCreated(opts mongo.CreateCollectionOptions) bool {
	m.muc.Lock()
	defer m.muc.Unlock()
	return slices.Contains(m.createCollectionOpts, opts)
}

func (m *mockMongoClient) WatchCollection(_ context.Context, opts *mongo.WatchCollectionOptions) error {
	if m.watchCollectionErr != nil {
		return m.watchCollectionErr
	}
	m.muw.Lock()
	defer m.muw.Unlock()
	m.watchCollectionOpts = append(m.watchCollectionOpts, *opts)
	return nil
}

func (m *mockMongoClient) CollectionWasWatched(opts mongo.WatchCollectionOptions) bool {
	m.muw.Lock()
	defer m.muw.Unlock()
	return slices.ContainsFunc(m.watchCollectionOpts, func(o mongo.WatchCollectionOptions) bool {
		return o.WatchedDbName == opts.WatchedDbName &&
			o.WatchedCollName == opts.WatchedCollName &&
			o.ResumeTokensDbName == opts.ResumeTokensDbName &&
			o.ResumeTokensCollName == opts.ResumeTokensCollName &&
			o.ResumeTokensCollCapped == opts.ResumeTokensCollCapped &&
			o.StreamName == opts.StreamName &&
			o.ChangeEventHandler != nil
	})
}

func (m *mockMongoClient) SimulateChangeEvents(subj, msgId string, data []byte) {
	m.muw.Lock()
	defer m.muw.Unlock()
	for _, opt := range m.watchCollectionOpts {
		_ = opt.ChangeEventHandler(context.Background(), subj, msgId, data)
	}
}

type mockNatsClient struct {
	closed     bool
	name       string
	monitorErr error

	mua           sync.Mutex
	addStreamOpts []nats.AddStreamOptions
	addStreamErr  error

	mup         sync.Mutex
	publishOpts []nats.PublishOptions
	publishErr  error
}

func (m *mockNatsClient) Close() error {
	m.closed = true
	return nil
}

func (m *mockNatsClient) Name() string {
	return m.name
}

func (m *mockNatsClient) Monitor(_ context.Context) error {
	return m.monitorErr
}

func (m *mockNatsClient) AddStream(_ context.Context, opts *nats.AddStreamOptions) error {
	if m.addStreamErr != nil {
		return m.addStreamErr
	}
	m.mua.Lock()
	defer m.mua.Unlock()
	m.addStreamOpts = append(m.addStreamOpts, *opts)
	return nil
}

func (m *mockNatsClient) StreamWasAdded(opt nats.AddStreamOptions) bool {
	m.mua.Lock()
	defer m.mua.Unlock()
	return slices.Contains(m.addStreamOpts, opt)
}

func (m *mockNatsClient) Publish(_ context.Context, opts *nats.PublishOptions) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.mup.Lock()
	defer m.mup.Unlock()
	m.publishOpts = append(m.publishOpts, *opts)
	return nil
}

func (m *mockNatsClient) MessageWasPublished(opt nats.PublishOptions) bool {
	m.mup.Lock()
	defer m.mup.Unlock()
	return slices.ContainsFunc(m.publishOpts, func(po nats.PublishOptions) bool {
		return po.Subj == opt.Subj && po.MsgId == opt.MsgId && bytes.Equal(po.Data, opt.Data)
	})
}
