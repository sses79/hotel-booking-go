package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	database "cloud.google.com/go/spanner/admin/database/apiv1"
	"cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	instance "cloud.google.com/go/spanner/admin/instance/apiv1"
	"cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"github.com/sses79/hotel-booking-go/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if os.Getenv("SPANNER_EMULATOR_HOST") == "" {
		log.Fatal("schema setup is local-only: SPANNER_EMULATOR_HOST is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := setup(ctx, cfg); err != nil {
		log.Fatal(err)
	}
	log.Printf("schema ready: %s", cfg.DatabasePath())
}

func setup(ctx context.Context, cfg config.Config) error {
	ddl, err := readDDL("schema/001_initial.sql")
	if err != nil {
		return err
	}
	if err := waitForEmulator(ctx, os.Getenv("SPANNER_EMULATOR_HOST")); err != nil {
		return err
	}
	if err := createInstance(ctx, cfg); err != nil {
		return err
	}
	return createDatabase(ctx, cfg, ddl)
}

func waitForEmulator(ctx context.Context, address string) error {
	for {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for Spanner emulator at %s: %w", address, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func readDDL(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var statements []string
	for _, part := range strings.Split(string(data), ";") {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	if len(statements) == 0 {
		return nil, errors.New("schema file is empty")
	}
	return statements, nil
}

func createInstance(ctx context.Context, cfg config.Config) error {
	admin, err := instance.NewInstanceAdminClient(ctx)
	if err != nil {
		return fmt.Errorf("connect to instance admin: %w", err)
	}
	defer admin.Close()
	op, err := admin.CreateInstance(ctx, &instancepb.CreateInstanceRequest{
		Parent:     "projects/" + cfg.Project,
		InstanceId: cfg.Instance,
		Instance: &instancepb.Instance{
			Name:        cfg.InstancePath(),
			Config:      "projects/" + cfg.Project + "/instanceConfigs/regional-us-central1",
			DisplayName: cfg.Instance,
			NodeCount:   1,
		},
	})
	if status.Code(err) == codes.AlreadyExists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}
	if _, err := op.Wait(ctx); err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("wait for instance: %w", err)
	}
	return nil
}

func createDatabase(ctx context.Context, cfg config.Config, ddl []string) error {
	admin, err := database.NewDatabaseAdminClient(ctx)
	if err != nil {
		return fmt.Errorf("connect to database admin: %w", err)
	}
	defer admin.Close()
	op, err := admin.CreateDatabase(ctx, &databasepb.CreateDatabaseRequest{
		Parent:          cfg.InstancePath(),
		CreateStatement: "CREATE DATABASE `" + cfg.Database + "`",
		ExtraStatements: ddl,
	})
	if status.Code(err) == codes.AlreadyExists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if _, err := op.Wait(ctx); err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("wait for database: %w", err)
	}
	return nil
}
