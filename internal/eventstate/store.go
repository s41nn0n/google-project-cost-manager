package eventstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
)

const (
	Accepted   = "accepted"
	Duplicate  = "duplicate"
	OutOfOrder = "out_of_order"
)

type Event struct {
	MessageID   string
	PublishTime time.Time
	CostAmount  float64
}

type Store interface {
	Claim(context.Context, string, Event) (string, error)
	Complete(context.Context, string, Event, bool) error
}

type record struct {
	MessageID   string    `firestore:"messageId"`
	PublishTime time.Time `firestore:"publishTime"`
	CostAmount  float64   `firestore:"costAmount"`
	Status      string    `firestore:"status"`
	UpdatedAt   time.Time `firestore:"updatedAt"`
}

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]record
}

func (m *MemoryStore) Claim(_ context.Context, key string, event Event) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[string]record{}
	}
	if previous, ok := m.records[key]; ok && previous.Status != "failed" {
		if event.MessageID != "" && previous.MessageID == event.MessageID {
			return Duplicate, nil
		}
		if !event.PublishTime.IsZero() && !previous.PublishTime.IsZero() && !event.PublishTime.After(previous.PublishTime) {
			return OutOfOrder, nil
		}
		if event.CostAmount < previous.CostAmount {
			return OutOfOrder, nil
		}
	}
	m.records[key] = record{MessageID: event.MessageID, PublishTime: event.PublishTime, CostAmount: event.CostAmount, Status: "processing", UpdatedAt: time.Now().UTC()}
	return Accepted, nil
}

func (m *MemoryStore) Complete(_ context.Context, key string, event Event, success bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := "completed"
	if !success {
		status = "failed"
	}
	if m.records == nil {
		m.records = map[string]record{}
	}
	m.records[key] = record{MessageID: event.MessageID, PublishTime: event.PublishTime, CostAmount: event.CostAmount, Status: status, UpdatedAt: time.Now().UTC()}
	return nil
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

func (s *FirestoreStore) Claim(ctx context.Context, key string, event Event) (string, error) {
	ref := s.client.Collection(s.collection).Doc(key)
	disposition := Accepted
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if err == nil {
			var previous record
			if err := snapshot.DataTo(&previous); err != nil {
				return err
			}
			if previous.Status != "failed" {
				if event.MessageID != "" && previous.MessageID == event.MessageID {
					disposition = Duplicate
					return nil
				}
				if !event.PublishTime.IsZero() && !previous.PublishTime.IsZero() && !event.PublishTime.After(previous.PublishTime) {
					disposition = OutOfOrder
					return nil
				}
				if event.CostAmount < previous.CostAmount {
					disposition = OutOfOrder
					return nil
				}
			}
		} else if !isNotFound(err) {
			return err
		}
		return tx.Set(ref, record{MessageID: event.MessageID, PublishTime: event.PublishTime, CostAmount: event.CostAmount, Status: "processing", UpdatedAt: time.Now().UTC()})
	})
	return disposition, err
}

func (s *FirestoreStore) Complete(ctx context.Context, key string, event Event, success bool) error {
	status := "completed"
	if !success {
		status = "failed"
	}
	_, err := s.client.Collection(s.collection).Doc(key).Set(ctx, record{MessageID: event.MessageID, PublishTime: event.PublishTime, CostAmount: event.CostAmount, Status: status, UpdatedAt: time.Now().UTC()})
	return err
}

func Key(budgetName, projectID, period string) string {
	sum := sha256.Sum256([]byte(budgetName + "\x00" + projectID + "\x00" + period))
	return hex.EncodeToString(sum[:])
}
