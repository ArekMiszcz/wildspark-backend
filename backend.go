package main

import (
	"context"
	"database/sql"

	"github.com/heroiclabs/nakama-common/runtime"
)

func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	// Register the game match
	if err := initializer.RegisterMatch("game", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (runtime.Match, error) {
		return &GameMatch{}, nil
	}); err != nil {
		logger.Error("unable to register game match: %v", err)
		return err
	}

	// Ensure the default game match exists
	if err := EnsureDefaultMatch(ctx, nk, logger); err != nil {
		logger.Error("failed to ensure default match exists: %v", err)
		return err
	}

	logger.Info("module loaded with game match, default match created")
	return nil
}
