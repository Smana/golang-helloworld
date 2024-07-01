package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strconv"
)

const (
	alpineImage = "alpine:3.20.1"
	golangImage = "golang:1.22"
	targetImage = "ttl.sh/golang-helloworld"
)

type GolangHelloworld struct{}

// Test runs go test on the provided source directory
func (m *GolangHelloworld) Test(ctx context.Context, source *Directory) (string, error) {
	ctr := dag.Container().From(golangImage)
	return ctr.
		WithWorkdir("/src").
		WithMountedDirectory("/src", source).
		WithExec([]string{"go", "test", "./..."}).
		Stdout(ctx)
}

// Build and publish Docker container
func (m *GolangHelloworld) Build(ctx context.Context, source *Directory) *Container {
	// build the binary
	builder := dag.Container().
		From(golangImage).
		WithDirectory("/src", source).
		WithWorkdir("/src").
		WithEnvVariable("CGO_ENABLED", "0").
		WithExec([]string{"go", "build", "-o", "helloworld", "cmd/helloworld/main.go"})

	// Create the target image with the binary
	targetImage := dag.Container().
		From(alpineImage).
		WithFile("/bin/helloworld", builder.File("/src/helloworld")).
		WithEntrypoint([]string{"/bin/helloworld"})

	return targetImage
}

// Publish the application container after building and testing it on-the-fly
func (m *GolangHelloworld) Publish(ctx context.Context, source *Directory) (string, error) {
	// call Dagger Function to run unit tests
	_, err := m.Test(ctx, source)
	if err != nil {
		return "", err
	}
	// publish the image to ttl.sh
	address, err := m.Build(ctx, source).
		Publish(ctx, fmt.Sprintf("%s-%.0f", targetImage, math.Floor(rand.Float64()*10000000))) //#nosec
	if err != nil {
		return "", err
	}
	return address, nil
}

// Serve builds and start a server for the provided source directory and a postgres database
func (m *GolangHelloworld) Serve(
	ctx context.Context,

	// The source directory of the application
	// +optional
	source *Directory,

	// The database name to use (if not set "wordsdb" is used)
	// +optional
	// +default="wordsdb"
	dbName string,

	// The postgres user secret
	pgUser *Secret,

	// The postgres password secret
	pgPass *Secret,

	// The pgPort to use (if not set "5432" is used)
	// +optional
	// +default="5432"
	pgPort string,

	// The directory containing the init script for the postgres database
	// +optional
	initScriptDir *Directory,

	// cache is a flag to enable caching of the postgres container
	// +optional
	// +default=false
	cache bool,

) (*Container, error) {

	if initScriptDir == nil {
		initScriptDir = source.Directory("./scripts")
	}

	opts := PostgresOpts{
		DbName:     dbName,
		Cache:      cache,
		Version:    "13",
		ConfigFile: nil,
		InitScript: initScriptDir,
	}
	// convert pgPort to int
	pgPortInt, err := strconv.Atoi(pgPort)
	if err != nil {
		return nil, fmt.Errorf("could not convert pgPort to int: %w", err)
	}
	pgCtr := dag.Postgres(pgUser, pgPass, pgPortInt, opts).Database()

	pgSvc := pgCtr.AsService()

	pgHostname, err := pgSvc.Hostname(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get postgres hostname: %w", err)
	}

	ctr := m.Build(ctx, source)

	return ctr.
		WithSecretVariable("PGPASSWORD", pgPass).
		WithSecretVariable("PGUSER", pgUser).
		WithEnvVariable("PGHOST", pgHostname).
		WithEnvVariable("PGDATABASE", opts.DbName).
		WithEnvVariable("PGPORT", pgPort).
		WithServiceBinding("database", pgSvc).
		WithExposedPort(8080), nil
}
