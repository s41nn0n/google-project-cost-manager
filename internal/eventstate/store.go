package eventstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

const (
	Accepted   = "accepted"
	Duplicate  = "duplicate"
	OutOfOrder = "out_of_order"
	Busy       = "busy"
	// Longer than the receiver's billing RPC deadline. A crashed worker is retryable.
	LeaseDuration = 2 * time.Minute
)

var ErrLostClaim = errors.New("event claim ownership lost or lease expired")

type Event struct {
	MessageID   string
	PublishTime time.Time
	CostAmount  float64
	ClaimToken  string
}

type Store interface {
	Claim(context.Context, string, *Event) (string, error)
	Complete(context.Context, string, Event, bool) error
}

type record struct {
	MessageID   string    `firestore:"messageId"`
	PublishTime time.Time `firestore:"publishTime"`
	CostAmount  float64   `firestore:"costAmount"`
	Status      string    `firestore:"status"`
	UpdatedAt   time.Time `firestore:"updatedAt"`
	LeaseUntil  time.Time `firestore:"leaseUntil"`
	ClaimToken  string    `firestore:"claimToken"`
}

func disposition(previous record, event Event, now time.Time) string {
	if previous.Status == "processing" && now.Before(previous.LeaseUntil) {
		return Busy
	}
	if previous.Status == "completed" && previous.MessageID == event.MessageID {
		return Duplicate
	}
	if previous.MessageID != event.MessageID && ((!previous.PublishTime.IsZero() && !event.PublishTime.After(previous.PublishTime)) || event.CostAmount < previous.CostAmount) {
		return OutOfOrder
	}
	return Accepted
}

func claimed(event Event, now time.Time) record {
	return record{MessageID: event.MessageID, PublishTime: event.PublishTime, CostAmount: event.CostAmount, Status: "processing", UpdatedAt: now, LeaseUntil: now.Add(LeaseDuration), ClaimToken: event.ClaimToken}
}

func finish(previous record, event Event, success bool, now time.Time) (record, error) {
	if previous.Status != "processing" || previous.ClaimToken != event.ClaimToken || event.ClaimToken == "" || !now.Before(previous.LeaseUntil) {
		return record{}, ErrLostClaim
	}
	previous.Status = "failed"
	if success {
		previous.Status = "completed"
	}
	previous.UpdatedAt = now
	return previous, nil
}

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]record
	now     func() time.Time
}

func (m *MemoryStore) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now().UTC()
}

func (m *MemoryStore) Claim(_ context.Context, key string, event *Event) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[string]record{}
	}
	now := m.clock()
	if previous, ok := m.records[key]; ok {
		if d := disposition(previous, *event, now); d != Accepted {
			return d, nil
		}
	}
	event.ClaimToken = uuid.NewString()
	m.records[key] = claimed(*event, now)
	return Accepted, nil
}

func (m *MemoryStore) Complete(_ context.Context, key string, event Event, success bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next, err := finish(m.records[key], event, success, m.clock())
	if err == nil {
		m.records[key] = next
	}
	return err
}

type FirestoreStore struct {
	client     *firestore.Client
	collection string
}

func NewFirestoreStore(ctx context.Context, projectID, databaseID, collection string) (*FirestoreStore, error) {
	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, err
	}
	return &FirestoreStore{client: client, collection: collection}, nil
}

func (s *FirestoreStore) Claim(ctx context.Context, key string, event *Event) (string, error) {
	ref := s.client.Collection(s.collection).Doc(key)
	event.ClaimToken = uuid.NewString()
	result := Accepted
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		result = Accepted
		now := time.Now().UTC()
		snapshot, err := tx.Get(ref)
		if err == nil {
			var previous record
			if err := snapshot.DataTo(&previous); err != nil {
				return err
			}
			result = disposition(previous, *event, now)
			if result != Accepted {
				return nil
			}
		} else if !isNotFound(err) {
			return err
		}
		return tx.Set(ref, claimed(*event, now))
	})
	return result, err
}

func (s *FirestoreStore) Complete(ctx context.Context, key string, event Event, success bool) error {
	ref := s.client.Collection(s.collection).Doc(key)
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var previous record
		if err := snapshot.DataTo(&previous); err != nil {
			return err
		}
		next, err := finish(previous, event, success, time.Now().UTC())
		if err != nil {
			return err
		}
		return tx.Set(ref, next)
	})
}

func Key(budgetName, projectID, period string) string {
	sum := sha256.Sum256([]byte(budgetName + "\x00" + projectID + "\x00" + period))
	return hex.EncodeToString(sum[:])
}
