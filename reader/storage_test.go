package reader

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shouni/go-remote-io/remoteio"
)

var (
	_ remoteio.Reader  = (*stubReader)(nil)
	_ io.Closer        = (*stubCloser)(nil)
	_ remoteio.Factory = (*stubFactory)(nil)
)

// stubReader は Open だけを差し替えたストアです。
//
// remoteio.Store を埋め込んでいるのは、reader が使うのが Open ひとつだけだからです。
// 他のメソッドを呼べば nil で落ちますが、それは「読む以外の操作はこのパッケージの
// 責務ではない」ことの表明でもあります。ビルド時の追従漏れは、実際に使う口である
// remoteio.Reader へのアサーションで検出します。
type stubReader struct {
	remoteio.Store

	content  string
	err      error
	lastPath string
}

func (s *stubReader) Open(_ context.Context, path string) (io.ReadCloser, error) {
	s.lastPath = path
	if s.err != nil {
		return nil, s.err
	}
	return io.NopCloser(strings.NewReader(s.content)), nil
}

type stubCloser struct {
	closed int
	err    error
}

func (s *stubCloser) Close() error {
	s.closed++
	return s.err
}

// remoteio.Factory を満足させるスタブ
type stubFactory struct {
	reader     remoteio.Store // 具象型ではなくインターフェースで保持する
	readerErr  error
	closeErr   error
	closeCalls int
}

func (s *stubFactory) Store() (remoteio.Store, error) {
	if s.readerErr != nil {
		return nil, s.readerErr
	}
	// ここが nil であれば、呼び出し側で store == nil として正しく判定される
	return s.reader, nil
}

// Handler は reader からは使われません。remoteio.Factory を満たすためだけの実装です。
func (s *stubFactory) Handler() (remoteio.Handler, error) { return nil, nil }

func (s *stubFactory) Close() error {
	s.closeCalls++
	return s.closeErr
}

// storageCache は、スキームに対応するキャッシュをテストから取り出します。
func storageCache(t *testing.T, r *UniversalReader, scheme string) *storageReaderCache {
	t.Helper()

	cache, ok := r.storages[scheme]
	if !ok {
		t.Fatalf("スキーム %q のストレージが登録されていません", scheme)
	}
	return cache
}

func TestOpenGCSUsesInjectedReader(t *testing.T) {
	t.Parallel()

	storageReader := &stubReader{content: "gcs body"}
	r := newTestReader(t, &stubExtractor{})
	storageCache(t, r, remoteio.SchemeGCS).reader = storageReader

	stream, err := r.Open(context.Background(), "gs://bucket/path.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "gcs body" {
		t.Fatalf("body = %q, want %q", got, "gcs body")
	}
	if storageReader.lastPath != "gs://bucket/path.txt" {
		t.Fatalf("reader.lastPath = %q", storageReader.lastPath)
	}
}

func TestOpenS3UsesInjectedReader(t *testing.T) {
	t.Parallel()

	storageReader := &stubReader{content: "s3 body"}
	r := newTestReader(t, &stubExtractor{})
	storageCache(t, r, remoteio.SchemeS3).reader = storageReader

	stream, err := r.Open(context.Background(), "s3://bucket/path.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := string(body); got != "s3 body" {
		t.Fatalf("body = %q, want %q", got, "s3 body")
	}
	if storageReader.lastPath != "s3://bucket/path.txt" {
		t.Fatalf("reader.lastPath = %q", storageReader.lastPath)
	}
}

// TestOpenStorageInitializesSchemesIndependently は、片方のスキームの初期化が
// 滞っていても、もう片方が独立して開けることを確認します。初期化には認証情報の
// 解決などの I/O が伴うため、ここを共有ロックにすると一方の遅延が他方を巻き込みます。
func TestOpenStorageInitializesSchemesIndependently(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	gcsStarted := make(chan struct{})

	r := newTestReader(t, &stubExtractor{},
		WithGCSFactory(func(context.Context) (remoteio.Factory, error) {
			close(gcsStarted)
			<-release // GCS の初期化を意図的に滞留させる
			return &stubFactory{reader: &stubReader{content: "gcs body"}}, nil
		}),
		WithS3Factory(func(context.Context) (remoteio.Factory, error) {
			return &stubFactory{reader: &stubReader{content: "s3 body"}}, nil
		}),
	)

	// 失敗時もテストが停止しないよう、滞留を解除してから Close する。
	// t.Cleanup は LIFO なので、後から登録した解除処理が先に走る。
	t.Cleanup(func() { _ = r.Close() })
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	go func() {
		stream, err := r.Open(context.Background(), "gs://bucket/blocked.txt")
		if err == nil {
			_ = stream.Close()
		}
	}()

	<-gcsStarted

	type result struct {
		body string
		err  error
	}
	s3Done := make(chan result, 1)
	go func() {
		stream, err := r.Open(context.Background(), "s3://bucket/path.txt")
		if err != nil {
			s3Done <- result{err: err}
			return
		}
		defer stream.Close()
		body, err := io.ReadAll(stream)
		s3Done <- result{body: string(body), err: err}
	}()

	select {
	case got := <-s3Done:
		if got.err != nil {
			t.Fatalf("Open(s3) error = %v", got.err)
		}
		if got.body != "s3 body" {
			t.Fatalf("body = %q, want %q", got.body, "s3 body")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("S3 の Open が GCS の初期化完了を待たされている")
	}
}

func TestOpenStorageReusesCachedReader(t *testing.T) {
	t.Parallel()

	var factoryCalls int
	r := newTestReader(t, &stubExtractor{},
		WithGCSFactory(func(context.Context) (remoteio.Factory, error) {
			factoryCalls++
			return &stubFactory{reader: &stubReader{content: "gcs body"}}, nil
		}),
	)
	defer func() { _ = r.Close() }()

	for i := range 3 {
		stream, err := r.Open(context.Background(), "gs://bucket/path.txt")
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		if err := stream.Close(); err != nil {
			t.Fatalf("stream.Close() error = %v", err)
		}
	}

	if factoryCalls != 1 {
		t.Fatalf("factoryCalls = %d, want 1", factoryCalls)
	}
}

func TestNewStorageReaderClosesFactoryOnReaderError(t *testing.T) {
	t.Parallel()

	factory := &stubFactory{
		readerErr: errors.New("reader failed"),
	}

	_, _, err := newStorageReader(context.Background(), func(context.Context) (remoteio.Factory, error) {
		return factory, nil
	})
	if err == nil {
		t.Fatal("newStorageReader() error = nil, want error")
	}
	if factory.closeCalls != 1 {
		t.Fatalf("factory.closeCalls = %d, want 1", factory.closeCalls)
	}
}

func TestNewStorageReaderClosesFactoryOnNilReader(t *testing.T) {
	t.Parallel()

	// reader フィールドが初期値 (nil) のままの状態。
	// これにより Store() が (remoteio.Store)(nil) を返すことをシミュレートする。
	factory := &stubFactory{}

	_, _, err := newStorageReader(context.Background(), func(context.Context) (remoteio.Factory, error) {
		return factory, nil
	})
	if err == nil {
		t.Fatal("newStorageReader() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "store is nil") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if factory.closeCalls != 1 {
		t.Fatalf("factory.closeCalls = %d, want 1", factory.closeCalls)
	}
}

func TestCloseClosesManagedResources(t *testing.T) {
	t.Parallel()

	gcsCloser := &stubCloser{}
	s3Closer := &stubCloser{}
	r := newTestReader(t, &stubExtractor{})
	gcsCache := storageCache(t, r, remoteio.SchemeGCS)
	s3Cache := storageCache(t, r, remoteio.SchemeS3)
	gcsCache.reader = &stubReader{}
	gcsCache.closer = gcsCloser
	s3Cache.reader = &stubReader{}
	s3Cache.closer = s3Closer

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if gcsCloser.closed != 1 || s3Closer.closed != 1 {
		t.Fatalf("close counts = (%d, %d), want (1, 1)", gcsCloser.closed, s3Closer.closed)
	}
	if gcsCache.reader != nil || gcsCache.closer != nil || s3Cache.reader != nil || s3Cache.closer != nil {
		t.Fatal("managed resources were not cleared")
	}
}

// Close は終端であり、解放後の Open は ErrClosed になること。
func TestOpenAfterCloseIsRejected(t *testing.T) {
	t.Parallel()

	r := newTestReader(t, &stubExtractor{})
	storageCache(t, r, remoteio.SchemeGCS).reader = &stubReader{content: "x"}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err := r.Open(context.Background(), "gs://bucket/path.txt")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Open() after Close error = %v, want ErrClosed", err)
	}
}

// Close 後の Open はファクトリを呼び直さないこと（新しい GCS クライアントを作らない）。
func TestOpenAfterCloseDoesNotReinitialize(t *testing.T) {
	t.Parallel()

	var factories []*stubFactory
	r := newTestReader(t, &stubExtractor{},
		WithGCSFactory(func(context.Context) (remoteio.Factory, error) {
			f := &stubFactory{reader: &stubReader{content: "gcs body"}}
			factories = append(factories, f)
			return f, nil
		}),
	)

	stream, err := r.Open(context.Background(), "gs://bucket/path.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close() error = %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := r.Open(context.Background(), "gs://bucket/path.txt"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Open() after Close error = %v, want ErrClosed", err)
	}

	if len(factories) != 1 {
		t.Fatalf("factory count = %d, want 1 (Close 後に作り直してはいけない)", len(factories))
	}
	if factories[0].closeCalls != 1 {
		t.Fatalf("factories[0].closeCalls = %d, want 1", factories[0].closeCalls)
	}

	// Close は冪等であること（多重解放でエラーにしない）。
	if err := r.Close(); err != nil {
		t.Fatalf("2 度目の Close() error = %v", err)
	}
	if factories[0].closeCalls != 1 {
		t.Fatalf("2 度目の Close で closeCalls = %d, want 1", factories[0].closeCalls)
	}
}

// 既定の URL 安全性検証を使ったまま gs:// / s3:// が開けること。
//
// securenet.ValidateURL（netarmor v1.3.0 以降）は http/https 以外を拒否するため、
// 全スキームを検証に通すと GCS/S3 が一切開けなくなる。既定の検証器を差し替えずに
// 確かめる唯一のテストなので、WithSafeURLValidator は渡さない。
func TestOpenStorageWithDefaultURLValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
		opt  func(StorageFactory) Option
	}{
		{name: "GCS", uri: "gs://bucket/path.txt", opt: WithGCSFactory},
		{name: "S3", uri: "s3://bucket/path.txt", opt: WithS3Factory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := func(context.Context) (remoteio.Factory, error) {
				return &stubFactory{reader: &stubReader{content: "storage body"}}, nil
			}
			r := New(WithExtractor(&stubExtractor{}), tt.opt(factory))
			defer func() { _ = r.Close() }()

			stream, err := r.Open(context.Background(), tt.uri)
			if err != nil {
				t.Fatalf("Open(%s) error = %v", tt.uri, err)
			}
			defer func() { _ = stream.Close() }()

			body, err := io.ReadAll(stream)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if got := string(body); got != "storage body" {
				t.Fatalf("body = %q, want %q", got, "storage body")
			}
		})
	}
}
