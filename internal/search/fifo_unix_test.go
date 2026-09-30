//go:build unix

package search

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestContentsSkipsPipes(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "needle"})
	// Opening a pipe blocks until something writes to it
	if err := syscall.Mkfifo(filepath.Join(root, "needle.fifo"), 0644); err != nil {
		t.Skipf("can't make a pipe: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		n := 0
		Contents(context.Background(), Options{Root: root}, "needle", func(Result) { n++ })
		done <- n
	}()
	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("got %d matches, want only a.txt", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("content search blocked on a pipe")
	}
}
