package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrObjectNotFound — объекта с таким ключом нет.
var ErrObjectNotFound = errors.New("storage: object not found")

// Storage abstracts object storage (S3-compatible). Implementations must be
// safe for concurrent use.
type Storage interface {
	// Put writes an object. size may be -1 if unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get opens an object for reading. Caller must Close the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// PresignGet returns a time-limited URL for downloading the object.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// PresignGetAttachment is PresignGet whose response carries the given
	// Content-Disposition (response-content-disposition, part of the
	// signature): the browser saves the file instead of playing it in a tab.
	PresignGetAttachment(ctx context.Context, key string, ttl time.Duration, disposition string) (string, error)
	// PresignPut returns a time-limited URL for uploading one object with a
	// single PUT. The uploader must send exactly this Content-Type: it is part
	// of the signature.
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	// Head reports the size and ETag (unquoted, lower case) of an object.
	// A missing object is ErrObjectNotFound.
	Head(ctx context.Context, key string) (size int64, etag string, err error)
	// Delete removes a single object. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every object whose key starts with prefix.
	DeletePrefix(ctx context.Context, prefix string) error
}

// OriginalKey builds the object key for an uploaded source file.
func OriginalKey(workID int64, filename string, ts int64) string {
	return fmt.Sprintf("works/%d/original/%d_%s", workID, ts, filename)
}

// PagePreviewKey builds the object key for a page preview image.
func PagePreviewKey(workID int64, pageNumber int) string {
	return fmt.Sprintf("works/%d/pages/page_%d.png", workID, pageNumber)
}
