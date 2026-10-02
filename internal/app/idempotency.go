package app

import (
	"context"
	"errors"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
	"github.com/example/google-project-cost-manager/internal/policy"
	"github.com/example/google-project-cost-manager/internal/pubsub"
)

func (s *Server) recordAlreadyDisabled(ctx context.Context, cfg *config.Config, envelope *pubsub.Envelope, decision *policy.Decision) error {
	start, err := envelope.Alert.PeriodStart()
	if err != nil {
		return err
	}
	store, err := s.eventStore(ctx, cfg.EventState)
	if err != nil {
		return err
	}
	event := eventstate.Event{MessageID: envelope.MessageID, PublishTime: envelope.PublishTime, CostAmount: envelope.Alert.CostAmount}
	key := eventstate.Key(decision.MatchedBudget, decision.ProjectID, start.UTC().Format("2006-01"))
	disposition, err := store.Claim(ctx, key, &event)
	if err != nil {
		return err
	}
	if disposition != eventstate.Accepted {
		if disposition == eventstate.Busy {
			return errors.New("unfinished event claim; retry delivery")
		}
		decision.Reason = disposition + "_event"
		return nil
	}
	return store.Complete(ctx, key, event, true)
}
