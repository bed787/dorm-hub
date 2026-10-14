package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"dorm-hub/internal/domain"
)

func TestAdmissionBoundsBuffersBeforeReading(t *testing.T) {
	s := New(nil, nil)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	done := make(chan error, 4)
	closed := false
	t.Cleanup(func() {
		if !closed {
			close(release)
		}
	})
	for i := 0; i < 4; i++ {
		go func() {
			_, err := s.UploadFrom(context.Background(), 1, 1, "", func() ([]byte, error) {
				entered <- struct{}{}
				<-release
				return nil, domain.Invalid("test reader stopped")
			})
			done <- err
		}()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("upload did not enter its reader")
		}
	}
	read := false
	_, err := s.UploadFrom(context.Background(), 1, 1, "", func() ([]byte, error) { read = true; return nil, nil })
	if read || !errors.Is(err, domain.ErrRateLimit) {
		t.Fatal("full admission queue read another body", err)
	}
	close(release)
	closed = true
	for i := 0; i < 4; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("upload did not release admission")
		}
	}
	_, err = s.UploadFrom(context.Background(), 1, 1, "", func() ([]byte, error) { return nil, domain.Invalid("released") })
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("released admission was not reusable", err)
	}
}
