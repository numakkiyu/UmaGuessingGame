package room

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func waitPending(t *testing.T, d *Dispatcher, id string, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for d.Pending(id) != count {
		if time.Now().After(deadline) {
			t.Fatalf("pending=%d, want %d", d.Pending(id), count)
		}
		time.Sleep(time.Millisecond)
	}
}
func TestQueueFIFOAndCancellation(t *testing.T) {
	d := NewDispatcherWithLimit(8, time.Second, 8)
	defer d.Close()
	_, release, err := d.Enqueue(context.Background(), "room")
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan int, 3)
	cancelled, cancel := context.WithCancel(context.Background())
	for i := 0; i < 3; i++ {
		ctx := context.Background()
		if i == 1 {
			ctx = cancelled
		}
		go func(index int, ctx context.Context) {
			_, done, err := d.Enqueue(ctx, "room")
			if err != nil {
				order <- -1
				return
			}
			order <- index
			done()
		}(i, ctx)
		waitPending(t, d, "room", i+2)
	}
	cancel()
	if got := <-order; got != -1 {
		t.Fatalf("cancelled waiter result %d", got)
	}
	waitPending(t, d, "room", 3)
	release()
	if got := <-order; got != 0 {
		t.Fatalf("first queued=%d", got)
	}
	if got := <-order; got != 2 {
		t.Fatalf("last queued=%d", got)
	}
	waitPending(t, d, "room", 0)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.rooms) != 0 || d.pending != 0 {
		t.Fatal("empty queue retained")
	}
}
func TestQueueGlobalBoundReclamationAndClose(t *testing.T) {
	d := NewDispatcherWithLimit(4, time.Second, 2)
	_, releaseA, err := d.Enqueue(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	_, releaseB, err := d.Enqueue(context.Background(), "b")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.Enqueue(context.Background(), "c"); err == nil {
		t.Fatal("global admission exceeded")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); releaseA() }()
	}
	wg.Wait()
	releaseB()
	for i := 0; i < 1000; i++ {
		_, done, err := d.Enqueue(context.Background(), fmt.Sprintf("room-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		done()
	}
	d.mu.Lock()
	retained := len(d.rooms)
	pending := d.pending
	d.mu.Unlock()
	if retained != 0 || pending != 0 {
		t.Fatalf("retained %d rooms, %d commands", retained, pending)
	}
	_, holder, err := d.Enqueue(context.Background(), "waiting")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, done, err := d.Enqueue(context.Background(), "waiting")
		if done != nil {
			done()
		}
		finished <- err
	}()
	waitPending(t, d, "waiting", 2)
	d.Close()
	d.Close()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("close admitted waiter")
		}
	case <-time.After(time.Second):
		t.Fatal("close stranded waiter")
	}
	holder()
	if _, _, err = d.Enqueue(context.Background(), "new-after-close"); err == nil {
		t.Fatal("closed dispatcher admitted command")
	}
}
