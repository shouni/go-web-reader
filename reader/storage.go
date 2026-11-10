package reader

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/shouni/go-remote-io/remoteio"
)

// storageReaderCache lazily initialises and holds the storage reader for one scheme.
//
// The lock is per scheme because initialisation involves I/O such as resolving credentials. With a
// shared lock, an S3 Open or even Close would wait while GCS initialisation is stuck.
//
// remoteio.Lazy cannot replace it. What is managed here is the Factory's lifetime (Close, and refusing
// use after release); Lazy only wraps a Handler and has no lifetime.
type storageReaderCache struct {
	label      string
	newFactory StorageFactory

	mu     sync.Mutex
	reader remoteio.Reader
	closer io.Closer
	closed bool
}

// openStorage gets the storage reader for the scheme and returns a read stream for the URI.
func (r *UniversalReader) openStorage(ctx context.Context, uri string, cache *storageReaderCache) (io.ReadCloser, error) {
	reader, err := cache.get(ctx)
	if err != nil {
		return nil, err
	}

	return reader.Open(ctx, uri)
}

// get lazily initialises the storage reader and reuses it on later calls.
func (c *storageReaderCache) get(ctx context.Context) (remoteio.Reader, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, fmt.Errorf("%s reader is not available: %w", c.label, ErrClosed)
	}
	if c.reader != nil {
		return c.reader, nil
	}

	reader, closer, err := newStorageReader(ctx, c.newFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s reader: %w", c.label, err)
	}
	c.reader = reader
	c.closer = closer

	return c.reader, nil
}

// newStorageReader creates the input reader and closer from a storage factory.
func newStorageReader(ctx context.Context, newFactory StorageFactory) (remoteio.Reader, io.Closer, error) {
	factory, err := newFactory(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create storage factory: %w", err)
	}

	// Only Open is needed, so the held type is narrowed to remoteio.Reader (the type says it does nothing beyond reading).
	store, err := factory.Store()
	if err == nil && store == nil {
		err = fmt.Errorf("store is nil")
	}
	if err != nil {
		// The factory is referenced only here, so close it before returning on failure.
		_ = factory.Close()
		return nil, nil, fmt.Errorf("failed to create reader: %w", err)
	}

	return store, factory, nil
}

// close closes the held closer and refuses further use.
// It is terminal, as is customary for io.Closer. If an Open after release silently re-initialised,
// the embedding code would see something it closed open a connection again.
func (c *storageReaderCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.reader = nil

	if c.closer == nil {
		return nil
	}

	err := c.closer.Close()
	c.closer = nil

	return err
}
