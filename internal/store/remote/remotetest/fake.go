// Package remotetest provides an in-memory fake of the S3 client for tests of
// the remote state backend.
//
// The fake enforces the semantics the real service would — conditional writes
// reject with the real error codes, missing objects return the real typed
// errors — so a test passing against it means the production code handles
// those cases, not that the fake was lenient.
package remotetest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Object is one stored S3 object.
type Object struct {
	Body []byte
	ETag string
}

// Fake is an in-memory Client. Objects are keyed "bucket/key". Tests may read
// and seed the maps directly between commands; the mutex only guards the
// method calls themselves.
type Fake struct {
	mu      sync.Mutex
	Objects map[string]Object
	// FailPut scripts an error for PutObject on a key, standing in for a
	// network or service failure at upload time.
	FailPut map[string]error
	// FailGetBody scripts a mid-stream failure: GetObject on the key succeeds
	// but its Body errors after yielding half the object, standing in for a
	// connection dropped during a download.
	FailGetBody map[string]error
	// PutCount records how many PutObject calls each key received (counting
	// scripted failures), so a test can assert that a read-only command never
	// uploaded.
	PutCount map[string]int
	etagSeq  int
}

// NewFake returns an empty fake.
func NewFake() *Fake {
	return &Fake{
		Objects:     make(map[string]Object),
		FailPut:     make(map[string]error),
		FailGetBody: make(map[string]error),
		PutCount:    make(map[string]int),
	}
}

func objectKey(bucket, key *string) string {
	return aws.ToString(bucket) + "/" + aws.ToString(key)
}

// GetObject implements remote.Client.
func (f *Fake) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := objectKey(in.Bucket, in.Key)
	obj, ok := f.Objects[key]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	body := io.Reader(bytes.NewReader(obj.Body))
	if err, scripted := f.FailGetBody[key]; scripted {
		body = io.MultiReader(bytes.NewReader(obj.Body[:len(obj.Body)/2]), failingReader{err})
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(body),
		ETag: aws.String(obj.ETag),
	}, nil
}

// failingReader errors on the first read, ending a scripted half-delivered
// body the way a dropped connection would.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// PutObject implements remote.Client, including If-None-Match and If-Match
// conditional-write rejection.
func (f *Fake) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := objectKey(in.Bucket, in.Key)
	f.PutCount[key]++

	if err, ok := f.FailPut[key]; ok {
		return nil, err
	}

	existing, exists := f.Objects[key]
	if aws.ToString(in.IfNoneMatch) == "*" && exists {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "at least one of the pre-conditions you specified did not hold"}
	}
	if match := aws.ToString(in.IfMatch); match != "" && (!exists || existing.ETag != match) {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "at least one of the pre-conditions you specified did not hold"}
	}

	body, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.etagSeq++
	obj := Object{Body: body, ETag: fmt.Sprintf("%q", fmt.Sprintf("etag-%d", f.etagSeq))}
	f.Objects[key] = obj
	return &s3.PutObjectOutput{ETag: aws.String(obj.ETag)}, nil
}

// DeleteObject implements remote.Client. Deleting an absent key succeeds, as
// it does on the real service.
func (f *Fake) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.Objects, objectKey(in.Bucket, in.Key))
	return &s3.DeleteObjectOutput{}, nil
}
