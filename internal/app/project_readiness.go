package app

import (
	"context"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/readiness"
)

func (s *Server) checkReadiness(ctx context.Context, cfg *config.Config, project string) error {
	if cfg.ReadinessMode == config.ReadinessPerProject {
		scoped, err := readiness.Scope(cfg, project)
		if err != nil {
			return err
		}
		return s.opt.ReadinessCheck(ctx, scoped)
	}
	return s.opt.ReadinessCheck(ctx, cfg)
}
