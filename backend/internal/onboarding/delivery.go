package onboarding

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log"
	"time"

	"ai-buddy/backend/internal/access"
)

const maxDeliveryAttempts = 5

// AccessService is the HTTP contract used by the worker.
type AccessService interface {
	Create(ctx context.Context, key string, payload []byte) (access.Ticket, error)
	Lookup(ctx context.Context, key string) (access.Ticket, error)
	Status(ctx context.Context, id string) (access.Ticket, error)
}

type Worker struct {
	store       *Store
	client      AccessService
	retryDelay  time.Duration
	statusDelay time.Duration
}

func NewWorker(store *Store, client AccessService) *Worker {
	return &Worker{store: store, client: client, retryDelay: time.Second, statusDelay: 2 * time.Second}
}

type deliveryJob struct {
	id, state, key, lease string
	payload               []byte
	externalID            *string
	attempts              int
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if _, err := w.step(ctx); err != nil && ctx.Err() == nil {
			log.Printf("access worker: storage error %T", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// claim acquires one job in a single statement and releases the database row
// before making any HTTP request. A lease token prevents a stale worker from
// overwriting the result of a later claim.
func (w *Worker) claim(ctx context.Context) (*deliveryJob, error) {
	job := &deliveryJob{lease: rand.Text()}
	err := w.store.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id, delivery_state FROM app_operations
		WHERE next_attempt_at <= now() AND (lease_until IS NULL OR lease_until <= now())
		  AND ((delivery_state IN ('queued', 'reconciling') AND attempts < $2)
		    OR delivery_state = 'sending'
		    OR (delivery_state = 'delivered' AND service_status = 'pending_approval'))
		ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
	)
	UPDATE app_operations o SET
		delivery_state = CASE c.delivery_state WHEN 'queued' THEN 'sending' WHEN 'sending' THEN 'reconciling' ELSE c.delivery_state END,
		attempts = o.attempts + CASE WHEN c.delivery_state = 'delivered' THEN 0 ELSE 1 END,
		lease_token = $1, lease_until = now() + interval '15 seconds'
	FROM candidate c WHERE o.id = c.id
	RETURNING o.id, c.delivery_state, o.idempotency_key, o.immutable_payload, o.external_id, o.attempts`, job.lease, maxDeliveryAttempts).
		Scan(&job.id, &job.state, &job.key, &job.payload, &job.externalID, &job.attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func (w *Worker) step(ctx context.Context) (bool, error) {
	job, err := w.claim(ctx)
	if err != nil || job == nil {
		return false, err
	}
	var ticket access.Ticket
	switch job.state {
	case "queued":
		ticket, err = w.client.Create(ctx, job.key, job.payload)
	case "delivered":
		if job.externalID == nil {
			return true, errors.New("delivered operation has no external id")
		}
		ticket, err = w.client.Status(ctx, *job.externalID)
		if err == nil && ticket.ID != *job.externalID {
			err = errors.New("external service returned a different request id")
		}
	default:
		// An expired sending lease is also an unknown outcome. Look up first.
		ticket, err = w.client.Lookup(ctx, job.key)
		if errors.Is(err, access.ErrNotFound) && job.attempts <= maxDeliveryAttempts {
			ticket, err = w.client.Create(ctx, job.key, job.payload)
		}
	}
	if err == nil {
		return true, w.save(ctx, job, "delivered", &ticket, w.statusDelay, false)
	}
	if job.state == "delivered" {
		// A failed status poll leaves the last confirmed result intact.
		return true, w.save(ctx, job, "delivered", nil, w.statusDelay, false)
	}
	return true, w.save(ctx, job, "reconciling", nil, w.retryDelay, errors.Is(err, access.ErrConflict))
}

func (w *Worker) save(ctx context.Context, job *deliveryJob, state string, ticket *access.Ticket, delay time.Duration, exhausted bool) error {
	var id, status *string
	if ticket != nil {
		id, status = &ticket.ID, &ticket.Status
	}
	_, err := w.store.db.ExecContext(ctx, `UPDATE app_operations SET
		delivery_state = $3,
		external_id = COALESCE($4, external_id), service_status = COALESCE($5, service_status),
		next_attempt_at = now() + make_interval(secs => $6),
		lease_token = NULL, lease_until = NULL,
		attempts = CASE WHEN $7 THEN GREATEST(attempts, $8) ELSE attempts END
		WHERE id = $1 AND lease_token = $2`, job.id, job.lease, state, id, status, delay.Seconds(), exhausted, maxDeliveryAttempts)
	return err
}
