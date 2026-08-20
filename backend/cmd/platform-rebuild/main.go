// Command platform-rebuild prepares an intentional my2.0 platform cutover.
// It is dry-run by default; use --apply only after a verified database backup.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
)

type rebuildCounts struct {
	AccountsWithPlatform int64
	ActivePlatforms      int64
	ActiveModelRules     int64
	APIKeyPlatforms      int64
}

func main() {
	apply := flag.Bool("apply", false, "apply the cutover (default is dry-run)")
	flag.Parse()

	cfg, err := config.LoadForBootstrap()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	client, db, err := repository.InitEnt(cfg)
	if err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	counts, err := readCounts(ctx, db)
	if err != nil {
		log.Fatalf("read rebuild counts: %v", err)
	}
	mode := "dry-run"
	if *apply {
		if err := applyCutover(ctx, db); err != nil {
			log.Fatalf("apply platform cutover: %v", err)
		}
		mode = "apply"
	}
	fmt.Printf("mode=%s accounts_with_platform=%d active_platforms=%d active_model_rules=%d api_key_platforms=%d\n",
		mode, counts.AccountsWithPlatform, counts.ActivePlatforms, counts.ActiveModelRules, counts.APIKeyPlatforms)
	if !*apply {
		fmt.Println("no data changed; rerun with --apply only after verifying a database backup")
	} else {
		fmt.Println("platform cutover applied; users, API keys, balances, plans, subscriptions, payments, and usage history were retained")
	}
}

func readCounts(ctx context.Context, db *sql.DB) (rebuildCounts, error) {
	var counts rebuildCounts
	queries := []struct {
		destination *int64
		query       string
	}{
		{&counts.AccountsWithPlatform, `SELECT COUNT(*) FROM accounts WHERE platform_id IS NOT NULL`},
		{&counts.ActivePlatforms, `SELECT COUNT(*) FROM platforms WHERE status = 'active'`},
		{&counts.ActiveModelRules, `SELECT COUNT(*) FROM platform_model_rules WHERE status = 'active'`},
		{&counts.APIKeyPlatforms, `SELECT COUNT(*) FROM api_key_platforms`},
	}
	for _, item := range queries {
		if err := db.QueryRowContext(ctx, item.query).Scan(item.destination); err != nil {
			return rebuildCounts{}, err
		}
	}
	return counts, nil
}

func applyCutover(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DELETE FROM api_key_platforms`,
		`UPDATE accounts SET platform_id = NULL, schedulable = FALSE, status = 'disabled' WHERE platform_id IS NOT NULL`,
		`UPDATE platform_model_rules SET status = 'disabled' WHERE status <> 'disabled'`,
		`UPDATE platforms SET status = 'disabled' WHERE status <> 'disabled'`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
