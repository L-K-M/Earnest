package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/L-K-M/Earnest/internal/config"
	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/service"
)

func evalCommand(ctx context.Context, args []string, w io.Writer) error {
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	task := flags.String("task", "summary", "summary, classify or edition")
	corpusPath := flags.String("corpus", "testdata/golden/items.json", "path to the original golden corpus")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*task != "summary" && *task != "classify" && *task != "edition") {
		return fmt.Errorf("usage: earnest eval --task summary|classify|edition [--corpus path]")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("eval configuration: %w", err)
	}
	models, err := llm.Load()
	if err != nil {
		return fmt.Errorf("eval models: %w", err)
	}
	egress, err := outbound.New(outbound.Config{Version: version, AllowHosts: cfg.OutboundAllowPrivate})
	if err != nil {
		return err
	}
	router, err := llm.NewRouter(models, egress)
	if err != nil {
		return err
	}
	if !router.Enabled(llm.Task(*task)) {
		return service.ErrPipelineDisabled
	}
	db, err := openDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	client, err := llm.NewClient(router, llm.NewBudget(db, models.Limits()), models.Models)
	if err != nil {
		return err
	}
	engine, err := pipeline.New(router, client, llm.NewCache(db), models.Models)
	if err != nil {
		return err
	}
	svc, err := service.New(db, service.Deps{SecretKey: cfg.SecretKey})
	if err != nil {
		return err
	}
	corpus, err := os.Open(*corpusPath)
	if err != nil {
		return fmt.Errorf("open golden corpus: %w", err)
	}
	defer corpus.Close()
	report, evalErr := svc.EvaluateGolden(service.WithPipeline(ctx, engine), llm.Task(*task), corpus)
	if err := json.NewEncoder(w).Encode(report); err != nil {
		return fmt.Errorf("write evaluation: %w", err)
	}
	if evalErr != nil {
		return evalErr
	}
	if report.Passed != report.Total {
		return fmt.Errorf("evaluation passed %d/%d cases", report.Passed, report.Total)
	}
	return nil
}
