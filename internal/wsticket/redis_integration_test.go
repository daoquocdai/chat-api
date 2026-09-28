package wsticket

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestRedisTicketAtomicConsumeIntegration(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRedisRepository(client), 30*time.Second)
	userID := uuid.NewString()
	ticket, err := service.Issue(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := service.Consume(ctx, ticket)
			if err == nil && got != userID {
				err = errors.New("wrong user")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInvalidTicket) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}

	short := NewService(NewRedisRepository(client), 50*time.Millisecond)
	expiring, err := short.Issue(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := short.Consume(ctx, expiring); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("expired ticket: %v", err)
	}
}
