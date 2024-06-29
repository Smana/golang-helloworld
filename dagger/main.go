package main

import (
	"context"
	"fmt"
)

type GolangHelloworld struct{}

// Test runs go test on the provided source directory
func (m *GolangHelloworld) Test(ctx context.Context, source *Directory) (string, error) {
	ctr := dag.Container().From("golang:1.22")
	return ctr.
		WithWorkdir("/mnt").
		WithMountedDirectory("/mnt", source).
		WithExec([]string{"go", "test", "./..."}).
		Stdout(ctx)
}

// Serve builds and start a server for the provided source directory and a postgres database
func (m *GolangHelloworld) Serve(ctx context.Context, source *Directory, pgUser *Secret, pgPass *Secret, initScriptDir *Directory) (*Container, error) {

	opts := PostgresOpts{
		DbName:     "wordsdb",
		Cache:      false,
		Version:    "13",
		ConfigFile: nil,
		InitScript: initScriptDir,
	}

	pgCtr := dag.Postgres(pgUser, pgPass, 5432, opts).Database().WithMountedDirectory("/docker-entrypoint-initdb.d", initScriptDir)

	pgSvc := pgCtr.AsService()

	pgHostname, err := pgSvc.Hostname(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get postgres hostname: %w", err)
	}

	ctr := dag.Container().From("golang:1.22")
	return ctr.
		WithWorkdir("/mnt").
		WithMountedDirectory("/mnt", source).
		WithSecretVariable("PGPASSWORD", pgPass).
		WithSecretVariable("PGUSER", pgUser).
		WithEnvVariable("PGHOST", pgHostname).
		WithEnvVariable("PGDATABASE", opts.DbName).
		WithEnvVariable("PGPORT", "5432").
		WithExec([]string{"go", "run", "cmd/helloworld/main.go"}).
		WithServiceBinding("database", pgSvc).
		WithExposedPort(8080), nil
}
